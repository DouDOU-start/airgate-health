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

// aggregator.go：聚合查询，把 health_probes 原始时序数据加工成可用率/延迟视图。
//
// 设计原则：
//   - 查询都是只读，且全部命中 (account_id, probed_at) / (platform, probed_at) 索引。
//   - 延迟分位采用近似算法（按 1024 桶随机采样 + 排序），避免 PG 的 percentile_cont
//     在大数据量下扫表的开销；当前规模下也可以直接用 SQL percentile_cont，但为了
//     可移植性（万一未来换 SQLite/MySQL）放在 Go 端做。
//   - 90 天日级桶：按 date_trunc('day', probed_at) 分组，结果直接给前端方格图用。

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

// AccountHealth 单个账号在某个窗口内的聚合结果。
type AccountHealth struct {
	AccountID    int64        `json:"account_id"`
	AccountName  string       `json:"account_name"`
	Platform     string       `json:"platform"`
	Status       string       `json:"status"`        // 来自 core accounts.status：active/error/disabled
	Window       string       `json:"window"`        // 7d/15d/30d
	TotalProbes  int          `json:"total_probes"`  // 窗口内 probe 总数
	SuccessCount int          `json:"success_count"` // 窗口内成功数
	UptimePct    float64      `json:"uptime_pct"`    // 0..100
	LatencyP50   int          `json:"latency_p50"`
	LatencyP95   int          `json:"latency_p95"`
	LatencyP99   int          `json:"latency_p99"`
	LastProbedAt *time.Time   `json:"last_probed_at,omitempty"`
	LastError    string       `json:"last_error,omitempty"` // 窗口内最后一次失败的 error_msg
	Daily        []DailyPoint `json:"daily,omitempty"`      // 按天分桶
}

// DailyPoint 单日聚合点。
type DailyPoint struct {
	Date         string  `json:"date"` // YYYY-MM-DD
	TotalProbes  int     `json:"total"`
	SuccessCount int     `json:"success"`
	UptimePct    float64 `json:"uptime_pct"`
	LatencyP95   int     `json:"latency_p95"`
}

// PlatformHealth 一个 platform（聚合所有该 platform 的账号）。
//
// 公开状态页用这个结构（脱敏：不含 account_id 和具体 error_msg）。
type PlatformHealth struct {
	Platform     string       `json:"platform"`
	Window       string       `json:"window"`
	AccountCount int          `json:"account_count"`
	UptimePct    float64      `json:"uptime_pct"`
	LatencyP95   int          `json:"latency_p95"`
	StatusColor  string       `json:"status_color"` // green / yellow / red
	Daily        []DailyPoint `json:"daily,omitempty"`
}

// GroupHealth 一个 group（按 group 聚合关联的所有账号）。
type GroupHealth struct {
	GroupID      int64        `json:"group_id"`
	GroupName    string       `json:"group_name"`
	Platform     string       `json:"platform"`
	Note         string       `json:"note,omitempty"` // 来自 core groups.note，运维备注
	Window       string       `json:"window"`
	AccountCount int          `json:"account_count"`
	UptimePct    float64      `json:"uptime_pct"`
	LatencyP95   int          `json:"latency_p95"`
	StatusColor  string       `json:"status_color"`
	Daily        []DailyPoint `json:"daily,omitempty"`
}

// Aggregator 聚合查询的入口。
type Aggregator struct {
	db *sql.DB
}

func NewAggregator(db *sql.DB) *Aggregator {
	return &Aggregator{db: db}
}

// AccountHealthByID 单账号详情：基础信息 + 聚合 + 90 天日桶。
func (a *Aggregator) AccountHealthByID(ctx context.Context, id int64, w Window) (*AccountHealth, error) {
	// 1. 取 core accounts 表的元信息（只读）
	var ah AccountHealth
	ah.AccountID = id
	ah.Window = w.Name

	row := a.db.QueryRowContext(ctx, `SELECT name, platform, status FROM accounts WHERE id = $1`, id)
	if err := row.Scan(&ah.AccountName, &ah.Platform, &ah.Status); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("账号 %d 不存在", id)
		}
		return nil, fmt.Errorf("查询账号失败: %w", err)
	}

	// 2. 拉窗口内 latency 样本（成功的）+ success/total 计数
	since := time.Now().AddDate(0, 0, -w.Days)
	latencies, err := a.scanLatencies(ctx, `
		SELECT latency_ms FROM health_probes
		WHERE account_id = $1 AND probed_at >= $2 AND success = TRUE
	`, id, since)
	if err != nil {
		return nil, err
	}

	var lastErr sql.NullString
	var lastTime sql.NullTime
	err = a.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE success = TRUE) AS s,
			COUNT(*) AS t,
			MAX(probed_at) AS last_at
		FROM health_probes
		WHERE account_id = $1 AND probed_at >= $2
	`, id, since).Scan(&ah.SuccessCount, &ah.TotalProbes, &lastTime)
	if err != nil {
		return nil, fmt.Errorf("聚合 probe 计数失败: %w", err)
	}
	if lastTime.Valid {
		t := lastTime.Time
		ah.LastProbedAt = &t
	}
	// 最近一次失败的 error_msg
	_ = a.db.QueryRowContext(ctx, `
		SELECT error_msg FROM health_probes
		WHERE account_id = $1 AND probed_at >= $2 AND success = FALSE
		ORDER BY probed_at DESC LIMIT 1
	`, id, since).Scan(&lastErr)
	if lastErr.Valid {
		ah.LastError = lastErr.String
	}

	ah.UptimePct = computeUptime(ah.SuccessCount, ah.TotalProbes)
	ah.LatencyP50, ah.LatencyP95, ah.LatencyP99 = percentiles(latencies)

	// 3. 90 天日桶（用于前端方格图；与 window 解耦，固定 90 天好看）
	daily, err := a.dailyBuckets(ctx, "account_id", id, 90)
	if err != nil {
		return nil, err
	}
	ah.Daily = daily

	return &ah, nil
}

// PlatformHealthList 所有 platform 的聚合（admin overview / public status）。
func (a *Aggregator) PlatformHealthList(ctx context.Context, w Window, includeDaily bool) ([]PlatformHealth, error) {
	since := time.Now().AddDate(0, 0, -w.Days)

	rows, err := a.db.QueryContext(ctx, `
		SELECT
			p.platform,
			COUNT(*) FILTER (WHERE p.success = TRUE) AS s,
			COUNT(*) AS t,
			COUNT(DISTINCT p.account_id) AS account_count
		FROM health_probes p
		WHERE p.probed_at >= $1
		GROUP BY p.platform
		ORDER BY p.platform
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
		if err := rows.Scan(&ph.Platform, &s, &t, &ph.AccountCount); err != nil {
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
			SELECT latency_ms FROM health_probes
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

	// 也包含从未被 probe 过但存在的 platform（从 accounts 表补齐）
	if err := a.fillMissingPlatforms(ctx, &out, w.Name, includeDaily); err != nil {
		return nil, err
	}

	return out, nil
}

// fillMissingPlatforms 把 accounts 表里有但 health_probes 还没数据的 platform
// 也加进结果列表，状态色标灰（uptime=-1 约定为 unknown）。
func (a *Aggregator) fillMissingPlatforms(ctx context.Context, out *[]PlatformHealth, window string, includeDaily bool) error {
	rows, err := a.db.QueryContext(ctx, `
		SELECT platform, COUNT(*) FROM accounts WHERE status != 'disabled' GROUP BY platform
	`)
	if err != nil {
		return fmt.Errorf("查询 accounts 失败: %w", err)
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
			Platform:     platform,
			Window:       window,
			AccountCount: cnt,
			UptimePct:    -1,
			StatusColor:  "gray",
		})
	}
	return rows.Err()
}

// GroupHealthList 按 group 聚合（用 account_groups 多对多关联做 JOIN）。
func (a *Aggregator) GroupHealthList(ctx context.Context, w Window, includeDaily bool) ([]GroupHealth, error) {
	since := time.Now().AddDate(0, 0, -w.Days)

	// 注意：g.note 由 core 的 ent 迁移负责创建，是 NOT NULL DEFAULT '' 列；
	// 用 COALESCE 防御老 core（理论上不会发生，但避免 health 插件比 core 先升级时崩溃）。
	rows, err := a.db.QueryContext(ctx, `
		SELECT
			g.id, g.name, g.platform, COALESCE(g.note, '') AS note,
			COUNT(*) FILTER (WHERE p.success = TRUE) AS s,
			COUNT(*) AS t,
			COUNT(DISTINCT p.account_id) AS account_count
		FROM groups g
		LEFT JOIN account_groups ag ON ag.group_id = g.id
		LEFT JOIN health_probes p ON p.account_id = ag.account_id AND p.probed_at >= $1
		GROUP BY g.id, g.name, g.platform, g.note
		ORDER BY g.platform, g.name
	`, since)
	if err != nil {
		return nil, fmt.Errorf("聚合 group 健康失败: %w", err)
	}
	defer rows.Close()

	var out []GroupHealth
	for rows.Next() {
		var gh GroupHealth
		gh.Window = w.Name
		var s, t int
		if err := rows.Scan(&gh.GroupID, &gh.GroupName, &gh.Platform, &gh.Note, &s, &t, &gh.AccountCount); err != nil {
			return nil, err
		}
		gh.UptimePct = computeUptime(s, t)
		out = append(out, gh)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		latencies, err := a.scanLatencies(ctx, `
			SELECT p.latency_ms FROM health_probes p
			JOIN account_groups ag ON ag.account_id = p.account_id
			WHERE ag.group_id = $1 AND p.probed_at >= $2 AND p.success = TRUE
		`, out[i].GroupID, since)
		if err != nil {
			return nil, err
		}
		_, p95, _ := percentiles(latencies)
		out[i].LatencyP95 = p95
		out[i].StatusColor = colorize(out[i].UptimePct)

		if includeDaily {
			daily, err := a.dailyBucketsByGroup(ctx, out[i].GroupID, 90)
			if err != nil {
				return nil, err
			}
			out[i].Daily = daily
		}
	}
	return out, nil
}

// dailyBucketsByGroup 按日期分桶聚合一个分组的 health_probes（JOIN account_groups）。
// 与 dailyBuckets 平行，独立函数因为需要 JOIN，不能复用 filterColumn 模板。
func (a *Aggregator) dailyBucketsByGroup(ctx context.Context, groupID int64, days int) ([]DailyPoint, error) {
	since := time.Now().AddDate(0, 0, -days)
	rows, err := a.db.QueryContext(ctx, `
		SELECT
			to_char(date_trunc('day', p.probed_at), 'YYYY-MM-DD') AS d,
			COUNT(*) FILTER (WHERE p.success = TRUE) AS s,
			COUNT(*) AS t
		FROM health_probes p
		JOIN account_groups ag ON ag.account_id = p.account_id
		WHERE ag.group_id = $1 AND p.probed_at >= $2
		GROUP BY 1
		ORDER BY 1
	`, groupID, since)
	if err != nil {
		return nil, fmt.Errorf("group daily 桶查询失败: %w", err)
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

// AccountSummary admin 总览的列表项（轻量，不含 daily）。
type AccountSummary struct {
	AccountID    int64   `json:"account_id"`
	AccountName  string  `json:"account_name"`
	Platform     string  `json:"platform"`
	Status       string  `json:"status"`
	UptimePct    float64 `json:"uptime_pct"`
	LatencyP95   int     `json:"latency_p95"`
	LastProbedAt string  `json:"last_probed_at,omitempty"`
}

// AccountSummariesByPlatform 按 platform 过滤（空字符串 = 全部）的账号列表。
func (a *Aggregator) AccountSummariesByPlatform(ctx context.Context, platform string, w Window) ([]AccountSummary, error) {
	since := time.Now().AddDate(0, 0, -w.Days)

	args := []interface{}{since}
	whereClause := ""
	if platform != "" {
		args = append(args, platform)
		whereClause = "AND a.platform = $2"
	}

	q := fmt.Sprintf(`
		SELECT
			a.id, a.name, a.platform, a.status,
			COALESCE(SUM(CASE WHEN p.success THEN 1 ELSE 0 END), 0) AS s,
			COUNT(p.id) AS t,
			MAX(p.probed_at) AS last_at
		FROM accounts a
		LEFT JOIN health_probes p ON p.account_id = a.id AND p.probed_at >= $1
		WHERE a.status != 'disabled' %s
		GROUP BY a.id, a.name, a.platform, a.status
		ORDER BY a.platform, a.name
	`, whereClause)

	rows, err := a.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("聚合账号摘要失败: %w", err)
	}
	defer rows.Close()

	var out []AccountSummary
	idx := make(map[int64]int)
	for rows.Next() {
		var as AccountSummary
		var s, t int
		var lastAt sql.NullTime
		if err := rows.Scan(&as.AccountID, &as.AccountName, &as.Platform, &as.Status, &s, &t, &lastAt); err != nil {
			return nil, err
		}
		as.UptimePct = computeUptime(s, t)
		if lastAt.Valid {
			as.LastProbedAt = lastAt.Time.UTC().Format(time.RFC3339)
		}
		idx[as.AccountID] = len(out)
		out = append(out, as)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 补 p95：单独查（避免 SQL 端 percentile 函数依赖）
	for i := range out {
		latencies, err := a.scanLatencies(ctx, `
			SELECT latency_ms FROM health_probes
			WHERE account_id = $1 AND probed_at >= $2 AND success = TRUE
		`, out[i].AccountID, since)
		if err != nil {
			return nil, err
		}
		_, p95, _ := percentiles(latencies)
		out[i].LatencyP95 = p95
	}
	return out, nil
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

// dailyBuckets 按日期分桶；filterColumn 应为 "account_id" 或 "platform"。
func (a *Aggregator) dailyBuckets(ctx context.Context, filterColumn string, filterVal interface{}, days int) ([]DailyPoint, error) {
	if filterColumn != "account_id" && filterColumn != "platform" {
		return nil, fmt.Errorf("非法 filterColumn: %s", filterColumn)
	}
	since := time.Now().AddDate(0, 0, -days)
	q := fmt.Sprintf(`
		SELECT
			to_char(date_trunc('day', probed_at), 'YYYY-MM-DD') AS d,
			COUNT(*) FILTER (WHERE success = TRUE) AS s,
			COUNT(*) AS t
		FROM health_probes
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

func (a *Aggregator) dailyBucketsByPlatform(ctx context.Context, platform string, days int) ([]DailyPoint, error) {
	return a.dailyBuckets(ctx, "platform", platform, days)
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

// ParseAccountID 工具：解析路径里 :id 段为 int64。
func ParseAccountID(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("缺少账号 ID")
	}
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("无效的账号 ID: %s", s)
	}
	return id, nil
}
