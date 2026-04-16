package health

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// aggregator.go：聚合查询，把 group_health_probes 时序数据加工成可用率/延迟视图。
//
// 设计原则：
//   - 查询都是只读，且全部命中 (group_id, probed_at) / (platform, probed_at) 索引。
//   - 延迟分位在 Go 端用 sort 做线性插值，避免 PG 的 percentile_cont 扫表开销；
//     探测频率是分组级（比旧账号级低一个数量级），样本量本来就小。
//   - 90 天日级桶：按 date_trunc('day', probed_at) 分组，结果直接给前端方格图用。
//
// 与旧版本的关键差异：
//   - 旧：聚合单位 = 账号；新：聚合单位 = 分组
//   - 旧：PlatformHealth.account_count；新：PlatformHealth.group_count
//   - 旧：AccountHealth/AccountSummary；新：直接删除这两个类型
//   - 所有 JOIN account_groups 的逻辑消失（探测本身已经是分组级 black-box）

// Window 表示一个聚合时间窗。
type Window struct {
	Days int    // 7 / 15 / 30 / 90
	Name string // "7d" / "15d" / "30d" / "90d"
}

func ParseWindow(s string) Window {
	switch s {
	case "15d":
		return Window{Days: 15, Name: "15d"}
	case "30d":
		return Window{Days: 30, Name: "30d"}
	case "90d":
		return Window{Days: 90, Name: "90d"}
	default:
		return Window{Days: 7, Name: "7d"}
	}
}

// DailyPoint 单日聚合点（按 date_trunc('day', probed_at) 分组）。
type DailyPoint struct {
	Date         string  `json:"date"` // YYYY-MM-DD
	TotalProbes  int     `json:"total"`
	SuccessCount int     `json:"success"`
	UptimePct    float64 `json:"uptime_pct"`
	LatencyP95   int     `json:"latency_p95"`
}

// HourlyPoint 单小时聚合点（按 date_trunc('hour', probed_at) 分组）。
//
// 与 DailyPoint 并存而不是替代：
//   - DailyPoint 用于"长期趋势"视图（90 天概览）
//   - HourlyPoint 用于"近期细节"视图（7 天 = 168 个柱子，公开状态页用此）
//
// 探测频率默认 5 分钟一次 → 每个小时桶约 12 个样本，可用率有 0~100% 之间的
// 13 个离散档位，足够区分"完全 OK / 偶发抖动 / 部分故障 / 完全宕机"。
// 比 15 分钟桶（3 个样本，只有 0/33/67/100% 四个档位）信号丰富得多。
type HourlyPoint struct {
	// Hour ISO8601 起始时间，格式 "YYYY-MM-DDTHH:00:00Z"。
	// 选 ISO8601 而非纯小时数是因为前端 hover 提示要展示具体时刻。
	Hour         string  `json:"hour"`
	TotalProbes  int     `json:"total"`
	SuccessCount int     `json:"success"`
	UptimePct    float64 `json:"uptime_pct"`
}

// PlatformHealth 一个 platform（聚合所有该 platform 下的分组探测）。
//
// 公开状态页用这个结构（脱敏：不含 group_id 和具体 error_msg）。
type PlatformHealth struct {
	Platform    string       `json:"platform"`
	Window      string       `json:"window"`
	GroupCount  int          `json:"group_count"`
	UptimePct   float64      `json:"uptime_pct"`
	LatencyP95  int          `json:"latency_p95"`
	StatusColor string       `json:"status_color"` // green / yellow / red / gray
	Daily       []DailyPoint `json:"daily,omitempty"`
}

// GroupHealth 一个 group 的聚合。
type GroupHealth struct {
	GroupID      int64         `json:"group_id"`
	GroupName    string        `json:"group_name"`
	Platform     string        `json:"platform"`
	Note         string        `json:"note,omitempty"` // 来自 core groups.note
	Window       string        `json:"window"`
	TotalProbes  int           `json:"total_probes"`
	SuccessCount int           `json:"success_count"`
	UptimePct    float64       `json:"uptime_pct"`
	LatencyP50   int           `json:"latency_p50"`
	LatencyP95   int           `json:"latency_p95"`
	LatencyP99   int           `json:"latency_p99"`
	LastProbedAt *time.Time    `json:"last_probed_at,omitempty"`
	LastError    string        `json:"last_error,omitempty"` // 最近一次失败的 error_msg
	StatusColor  string        `json:"status_color"`
	Daily        []DailyPoint  `json:"daily,omitempty"`  // 90 天日桶（长期趋势用）
	Hourly       []HourlyPoint `json:"hourly,omitempty"` // 168 小时桶 = 7 天（公开状态页用）
}

// Aggregator 聚合查询的入口。
type Aggregator struct {
	db *sql.DB
}

func NewAggregator(db *sql.DB) *Aggregator {
	return &Aggregator{db: db}
}

// GroupHealthByID 单分组详情：基础信息 + 聚合 + 90 天日桶。
func (a *Aggregator) GroupHealthByID(ctx context.Context, id int64, w Window) (*GroupHealth, error) {
	// 1. 取 core groups 表的元信息（只读）
	var gh GroupHealth
	gh.GroupID = id
	gh.Window = w.Name

	row := a.db.QueryRowContext(ctx, `SELECT name, platform, COALESCE(note, '') FROM groups WHERE id = $1`, id)
	if err := row.Scan(&gh.GroupName, &gh.Platform, &gh.Note); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("分组 %d 不存在", id)
		}
		return nil, fmt.Errorf("查询分组失败: %w", err)
	}

	// 2. 拉窗口内 latency 样本（成功的）+ success/total 计数
	since := time.Now().AddDate(0, 0, -w.Days)
	latencies, err := a.scanLatencies(ctx, `
		SELECT latency_ms FROM group_health_probes
		WHERE group_id = $1 AND probed_at >= $2 AND success = TRUE
	`, id, since)
	if err != nil {
		return nil, err
	}

	var lastTime sql.NullTime
	err = a.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE success = TRUE) AS s,
			COUNT(*) AS t,
			MAX(probed_at) AS last_at
		FROM group_health_probes
		WHERE group_id = $1 AND probed_at >= $2
	`, id, since).Scan(&gh.SuccessCount, &gh.TotalProbes, &lastTime)
	if err != nil {
		return nil, fmt.Errorf("聚合 probe 计数失败: %w", err)
	}
	if lastTime.Valid {
		t := lastTime.Time
		gh.LastProbedAt = &t
	}
	// 最近一次失败的 error_msg
	var lastErr sql.NullString
	_ = a.db.QueryRowContext(ctx, `
		SELECT error_msg FROM group_health_probes
		WHERE group_id = $1 AND probed_at >= $2 AND success = FALSE
		ORDER BY probed_at DESC LIMIT 1
	`, id, since).Scan(&lastErr)
	if lastErr.Valid {
		gh.LastError = lastErr.String
	}

	gh.UptimePct = computeUptime(gh.SuccessCount, gh.TotalProbes)
	gh.LatencyP50, gh.LatencyP95, gh.LatencyP99 = percentiles(latencies)
	gh.StatusColor = colorize(gh.UptimePct)

	// 3. 90 天日桶（与 window 解耦，固定 90 天）
	daily, err := a.dailyBucketsByGroup(ctx, id, 90)
	if err != nil {
		return nil, err
	}
	gh.Daily = daily

	return &gh, nil
}

// PlatformHealthList 所有 platform 的聚合（admin overview / public status）。
//
// 从 group_health_probes 按 platform 聚合，然后用 groups 表的 platform count
// 补齐那些还没被探测过的平台（uptime = -1, gray）。
func (a *Aggregator) PlatformHealthList(ctx context.Context, w Window, includeDaily bool) ([]PlatformHealth, error) {
	since := time.Now().AddDate(0, 0, -w.Days)

	rows, err := a.db.QueryContext(ctx, `
		SELECT
			platform,
			COUNT(*) FILTER (WHERE success = TRUE) AS s,
			COUNT(*) AS t,
			COUNT(DISTINCT group_id) AS group_count
		FROM group_health_probes
		WHERE probed_at >= $1
		GROUP BY platform
		ORDER BY platform
	`, since)
	if err != nil {
		return nil, fmt.Errorf("聚合平台健康失败: %w", err)
	}
	defer rows.Close()

	var out []PlatformHealth
	for rows.Next() {
		var ph PlatformHealth
		ph.Window = w.Name
		var s, t int
		if err := rows.Scan(&ph.Platform, &s, &t, &ph.GroupCount); err != nil {
			return nil, err
		}
		ph.UptimePct = computeUptime(s, t)
		out = append(out, ph)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 补充每个平台的 p95 + 状态色 + (可选) daily
	for i := range out {
		latencies, err := a.scanLatencies(ctx, `
			SELECT latency_ms FROM group_health_probes
			WHERE platform = $1 AND probed_at >= $2 AND success = TRUE
		`, out[i].Platform, since)
		if err != nil {
			return nil, err
		}
		_, p95, _ := percentiles(latencies)
		out[i].LatencyP95 = p95
		out[i].StatusColor = colorize(out[i].UptimePct)

		if includeDaily {
			daily, err := a.dailyBucketsByPlatform(ctx, out[i].Platform, 90)
			if err != nil {
				return nil, err
			}
			out[i].Daily = daily
		}
	}

	// 也包含从未被 probe 过但存在的 platform（从 groups 表补齐）
	if err := a.fillMissingPlatforms(ctx, &out, w.Name); err != nil {
		return nil, err
	}

	return out, nil
}

// fillMissingPlatforms 把 groups 表里有但 group_health_probes 还没数据的 platform
// 也加进结果列表，状态色标灰（uptime=-1 约定为 unknown）。
func (a *Aggregator) fillMissingPlatforms(ctx context.Context, out *[]PlatformHealth, window string) error {
	rows, err := a.db.QueryContext(ctx, `
		SELECT platform, COUNT(*) FROM groups GROUP BY platform
	`)
	if err != nil {
		return fmt.Errorf("查询 groups 失败: %w", err)
	}
	defer rows.Close()

	have := make(map[string]bool, len(*out))
	for _, ph := range *out {
		have[ph.Platform] = true
	}
	for rows.Next() {
		var platform string
		var cnt int
		if err := rows.Scan(&platform, &cnt); err != nil {
			return err
		}
		if have[platform] {
			continue
		}
		*out = append(*out, PlatformHealth{
			Platform:    platform,
			Window:      window,
			GroupCount:  cnt,
			UptimePct:   -1,
			StatusColor: "gray",
		})
	}
	return rows.Err()
}

// GroupHealthList 所有 group 的聚合（从 core groups 表出发，LEFT JOIN 探测数据）。
//
// 用 LEFT JOIN 而不是只查 group_health_probes，是为了：
//   - 新加的分组即使还没被探测过，也要显示（状态灰）
//   - 被删除的分组不会再出现（group_health_probes 里虽有历史数据但 JOIN 失败）
//
// hourlyHours > 0 时额外填充 GroupHealth.Hourly（按小时分桶，最近 N 小时）。
// 公开状态页传 168（7 天 × 24 小时）；admin 视图传 0 跳过。
//
// 可见性过滤三档：
//   - publicOnly=false：不过滤（admin 视图，看到全部分组）
//   - publicOnly=true, userID=0：仅 g.status_visible = TRUE（匿名公开页）
//   - publicOnly=true, userID>0：上一条 OR 用户在 user_allowed_groups 里有记录
//     （登录用户，看到公开分组 + 自己被授权的专属分组，哪怕该专属分组 status_visible=false）
func (a *Aggregator) GroupHealthList(ctx context.Context, w Window, includeDaily bool, hourlyHours int, publicOnly bool, userID int) ([]GroupHealth, error) {
	since := time.Now().AddDate(0, 0, -w.Days)

	visibilityFilter := ""
	args := []interface{}{since}
	if publicOnly {
		if userID > 0 {
			// $2 是 userID；EXISTS 子查询命中 (user_id, group_id) 主键索引，成本 O(1)。
			visibilityFilter = `WHERE g.status_visible = TRUE OR EXISTS (
				SELECT 1 FROM user_allowed_groups uag
				WHERE uag.user_id = $2 AND uag.group_id = g.id
			)`
			args = append(args, userID)
		} else {
			visibilityFilter = "WHERE g.status_visible = TRUE"
		}
	}

	rows, err := a.db.QueryContext(ctx, `
		SELECT
			g.id, g.name, g.platform, COALESCE(g.note, '') AS note,
			COUNT(*) FILTER (WHERE p.success = TRUE) AS s,
			COUNT(p.id) AS t,
			MAX(p.probed_at) AS last_at
		FROM groups g
		LEFT JOIN group_health_probes p ON p.group_id = g.id AND p.probed_at >= $1
		`+visibilityFilter+`
		GROUP BY g.id, g.name, g.platform, g.note
		ORDER BY g.platform, g.name
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("聚合 group 健康失败: %w", err)
	}
	defer rows.Close()

	var out []GroupHealth
	for rows.Next() {
		var gh GroupHealth
		gh.Window = w.Name
		var lastAt sql.NullTime
		if err := rows.Scan(&gh.GroupID, &gh.GroupName, &gh.Platform, &gh.Note,
			&gh.SuccessCount, &gh.TotalProbes, &lastAt); err != nil {
			return nil, err
		}
		gh.UptimePct = computeUptime(gh.SuccessCount, gh.TotalProbes)
		if lastAt.Valid {
			t := lastAt.Time
			gh.LastProbedAt = &t
		}
		out = append(out, gh)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 补 latency percentiles + 最近错误 + (可选) daily
	for i := range out {
		latencies, err := a.scanLatencies(ctx, `
			SELECT latency_ms FROM group_health_probes
			WHERE group_id = $1 AND probed_at >= $2 AND success = TRUE
		`, out[i].GroupID, since)
		if err != nil {
			return nil, err
		}
		out[i].LatencyP50, out[i].LatencyP95, out[i].LatencyP99 = percentiles(latencies)
		out[i].StatusColor = colorize(out[i].UptimePct)

		// 最近一次失败的 error_msg（仅当存在失败时）
		if out[i].TotalProbes > 0 && out[i].SuccessCount < out[i].TotalProbes {
			var lastErr sql.NullString
			_ = a.db.QueryRowContext(ctx, `
				SELECT error_msg FROM group_health_probes
				WHERE group_id = $1 AND probed_at >= $2 AND success = FALSE
				ORDER BY probed_at DESC LIMIT 1
			`, out[i].GroupID, since).Scan(&lastErr)
			if lastErr.Valid {
				out[i].LastError = lastErr.String
			}
		}

		if includeDaily {
			daily, err := a.dailyBucketsByGroup(ctx, out[i].GroupID, 90)
			if err != nil {
				return nil, err
			}
			out[i].Daily = daily
		}
		if hourlyHours > 0 {
			hourly, err := a.hourlyBucketsByGroup(ctx, out[i].GroupID, hourlyHours)
			if err != nil {
				return nil, err
			}
			out[i].Hourly = hourly
		}
	}
	return out, nil
}

// ============================================================================
// 日桶辅助
// ============================================================================

func (a *Aggregator) dailyBucketsByGroup(ctx context.Context, groupID int64, days int) ([]DailyPoint, error) {
	return a.dailyBuckets(ctx, "group_id", groupID, days)
}

func (a *Aggregator) dailyBucketsByPlatform(ctx context.Context, platform string, days int) ([]DailyPoint, error) {
	return a.dailyBuckets(ctx, "platform", platform, days)
}

// hourlyBucketsByGroup 按小时分桶聚合一个分组最近 hours 小时的探测数据。
//
// 与 dailyBucketsByGroup 的区别：
//   - 用 date_trunc('hour', ...) 而非 'day'
//   - 时间窗口用 NOW() - INTERVAL，按小时倒推
//   - 返回的 Hour 字段是 ISO8601 时间戳字符串，便于前端 hover 展示具体时刻
//
// SQL 不主动填充"无数据"小时（COUNT(*)=0 的桶不会出现在 GROUP BY 结果里）。
// 让前端按 hours 数值生成完整时间轴，从结果集里查找对应桶填充——这样即使
// 某个小时完全没探测，UI 也能渲染一个"无数据"状态的灰色柱子。
func (a *Aggregator) hourlyBucketsByGroup(ctx context.Context, groupID int64, hours int) ([]HourlyPoint, error) {
	if hours <= 0 {
		return nil, nil
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	rows, err := a.db.QueryContext(ctx, `
		SELECT
			to_char(date_trunc('hour', probed_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:00:00"Z"') AS h,
			COUNT(*) FILTER (WHERE success = TRUE) AS s,
			COUNT(*) AS t
		FROM group_health_probes
		WHERE group_id = $1 AND probed_at >= $2
		GROUP BY 1
		ORDER BY 1
	`, groupID, since)
	if err != nil {
		return nil, fmt.Errorf("hourly 桶查询失败: %w", err)
	}
	defer rows.Close()
	var out []HourlyPoint
	for rows.Next() {
		var hp HourlyPoint
		var s, t int
		if err := rows.Scan(&hp.Hour, &s, &t); err != nil {
			return nil, err
		}
		hp.SuccessCount, hp.TotalProbes = s, t
		hp.UptimePct = computeUptime(s, t)
		out = append(out, hp)
	}
	return out, rows.Err()
}

// dailyBuckets 按日期分桶；filterColumn 应为 "group_id" 或 "platform"。
func (a *Aggregator) dailyBuckets(ctx context.Context, filterColumn string, filterVal interface{}, days int) ([]DailyPoint, error) {
	if filterColumn != "group_id" && filterColumn != "platform" {
		return nil, fmt.Errorf("非法 filterColumn: %s", filterColumn)
	}
	since := time.Now().AddDate(0, 0, -days)
	q := fmt.Sprintf(`
		SELECT
			to_char(date_trunc('day', probed_at), 'YYYY-MM-DD') AS d,
			COUNT(*) FILTER (WHERE success = TRUE) AS s,
			COUNT(*) AS t
		FROM group_health_probes
		WHERE %s = $1 AND probed_at >= $2
		GROUP BY 1
		ORDER BY 1
	`, filterColumn)
	rows, err := a.db.QueryContext(ctx, q, filterVal, since)
	if err != nil {
		return nil, fmt.Errorf("daily 桶查询失败: %w", err)
	}
	defer rows.Close()
	var out []DailyPoint
	for rows.Next() {
		var dp DailyPoint
		var s, t int
		if err := rows.Scan(&dp.Date, &s, &t); err != nil {
			return nil, err
		}
		dp.SuccessCount, dp.TotalProbes = s, t
		dp.UptimePct = computeUptime(s, t)
		out = append(out, dp)
	}
	return out, rows.Err()
}

// ============================================================================
// 工具函数
// ============================================================================

// scanLatencies 扫一列 latency_ms 到 []int。
func (a *Aggregator) scanLatencies(ctx context.Context, sqlStr string, args ...interface{}) ([]int, error) {
	rows, err := a.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("查询 latency 失败: %w", err)
	}
	defer rows.Close()
	out := make([]int, 0, 256)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// computeUptime 返回 0..100 的百分比；t==0 时返回 -1（约定 unknown）。
func computeUptime(success, total int) float64 {
	if total == 0 {
		return -1
	}
	v := float64(success) / float64(total) * 100
	return math.Round(v*100) / 100 // 保留两位小数
}

// percentiles 计算 p50/p95/p99（线性插值）。空切片返回 0,0,0。
func percentiles(xs []int) (p50, p95, p99 int) {
	if len(xs) == 0 {
		return 0, 0, 0
	}
	sort.Ints(xs)
	pick := func(p float64) int {
		if len(xs) == 1 {
			return xs[0]
		}
		idx := p * float64(len(xs)-1)
		lo := int(math.Floor(idx))
		hi := int(math.Ceil(idx))
		if lo == hi {
			return xs[lo]
		}
		frac := idx - float64(lo)
		return int(math.Round(float64(xs[lo])*(1-frac) + float64(xs[hi])*frac))
	}
	return pick(0.50), pick(0.95), pick(0.99)
}

// colorize 把 uptime% 映射到三色状态。
//   - >=99.5% green
//   - >=95%   yellow
//   - 其他    red
//   - -1      gray (unknown)
func colorize(uptime float64) string {
	switch {
	case uptime < 0:
		return "gray"
	case uptime >= 99.5:
		return "green"
	case uptime >= 95:
		return "yellow"
	default:
		return "red"
	}
}

// ParseGroupID 工具：解析路径里 :id 段为 int64。
func ParseGroupID(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("缺少分组 ID")
	}
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("无效的分组 ID: %s", s)
	}
	return id, nil
}
