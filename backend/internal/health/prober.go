package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// prober.go：分组级主动探测器（Step 2 重写）。
//
// 工作循环：
//  1. 通过 Host.Invoke("groups.list") 拿到 core 当前所有分组
//  2. 用 worker pool（受 concurrency 限制）对每个分组调 Host.Invoke("probe.forward")
//     —— 这是 core 侧"调度选号 + gateway.Forward + scheduler.ReportResult"
//     的包装，跳过 usage_log 写入和用户余额扣款，但让账号状态机受益
//  3. 把结果写一行到 group_health_probes
//  4. 抖动避免雪崩
//
// 与 Step 1 之前的 v1 实现的关键差异：
//   - 目标从账号粒度变成分组粒度（数量骤减：N 账号 → M 分组，M << N）
//   - 不再通过 admin HTTP API 调 /admin/accounts/:id/test
//   - 不再需要 admin_api_key 配置，走 hashicorp/go-plugin GRPCBroker 反向通道
//   - 探测走和真实用户请求**完全相同的路径**（scheduler.SelectAccount →
//     gateway.Forward），因此 uptime 直接反映"真实用户此刻能不能用"
//
// 不在这里实现 leader election：与 airgate 当前部署一致（单实例假设）。
// 多副本部署如果部署多份本插件，会出现重复探测——但表只是 append，数据正确性
// 不受影响，只是浪费上游配额。

const (
	hostMethodGroupsList   = "groups.list"
	hostMethodProbeForward = "probe.forward"
)

type hostGroup struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	IsExclusive    bool    `json:"is_exclusive"`
	RateMultiplier float64 `json:"rate_multiplier"`
	Note           string  `json:"note"`
	StatusVisible  bool    `json:"status_visible"`
}

type hostProbeForwardResult struct {
	Success    bool   `json:"success"`
	LatencyMs  int64  `json:"latency_ms"`
	StatusCode int64  `json:"status_code"`
	AccountID  int64  `json:"account_id"`
	Model      string `json:"model"`
	ErrorKind  string `json:"error_kind"`
	ErrorMsg   string `json:"error_msg"`
}

type ProberOptions struct {
	Interval    time.Duration // 主循环间隔；默认 300s（5 分钟）
	Concurrency int           // 同时进行的 probe.forward 上限；默认 4
	Jitter      time.Duration // 每次循环开头随机等待 [0, Jitter)；默认 5s
}

type Prober struct {
	logger *slog.Logger
	db     *sql.DB
	host   sdk.Host
	opts   ProberOptions

	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
	doneChan chan struct{}

	// failureMu 保护 consecutiveFailures；用于聚合"连续失败 → unhealthy/recovered"日志事件。
	// 仅影响日志，不影响数据落库或调度状态。
	failureMu           sync.Mutex
	consecutiveFailures map[int64]int
}

// NewProber 构造分组级探测器。
//
// host 是 sdk.HostAware 暴露的 core 反向调用接口；nil 时 Start 会变成 no-op
// （插件以"未启用探测"模式运行，aggregator 仍可读历史数据）。
func NewProber(logger *slog.Logger, db *sql.DB, host sdk.Host, opts ProberOptions) *Prober {
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Minute
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.Jitter < 0 {
		opts.Jitter = 5 * time.Second
	}
	return &Prober{
		logger: logger.With("component", "prober"),
		db:     db,
		host:   host,
		opts:   opts,
	}
}

// Start 启动探测主循环。可重复调用，已运行时是 no-op。
// host == nil 时立刻返回（插件配置不完整时允许跳过）。
func (p *Prober) Start(parent context.Context) {
	if p.host == nil {
		p.logger.Warn("host service 不可用，探测循环不会启动")
		return
	}
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.stopChan = make(chan struct{})
	p.doneChan = make(chan struct{})
	p.mu.Unlock()

	go p.loop(parent)
}

// Stop 停止主循环并等待退出。
func (p *Prober) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	close(p.stopChan)
	done := p.doneChan
	p.running = false
	p.mu.Unlock()
	<-done
}

func (p *Prober) hostInvoke(ctx context.Context, method string, payload map[string]interface{}) (map[string]interface{}, error) {
	return invokeHost(ctx, p.host, method, payload)
}

func decodeHostValue[T any](payload map[string]interface{}, out *T, keys ...string) error {
	var raw interface{} = payload
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			raw = value
			break
		}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return err
	}
	return nil
}

func (p *Prober) listGroups(ctx context.Context) ([]hostGroup, error) {
	return fetchGroups(ctx, p.host, false, 0)
}

func (p *Prober) probeForward(ctx context.Context, groupID int64) (*hostProbeForwardResult, error) {
	payload, err := p.hostInvoke(ctx, hostMethodProbeForward, map[string]interface{}{
		"group_id": groupID,
	})
	if err != nil {
		return nil, err
	}
	var result hostProbeForwardResult
	if err := decodeHostValue(payload, &result); err != nil {
		return nil, fmt.Errorf("解析 probe.forward 响应失败: %w", err)
	}
	return &result, nil
}

// loop 主循环：每 Interval 触发一次 RunOnce。
func (p *Prober) loop(parent context.Context) {
	defer close(p.doneChan)

	// 启动后短暂延迟一次，避免 core 还在装载其它插件时立刻打探测
	select {
	case <-time.After(3 * time.Second):
	case <-p.stopChan:
		return
	case <-parent.Done():
		return
	}

	for {
		// 抖动
		if p.opts.Jitter > 0 {
			d := time.Duration(rand.Int63n(int64(p.opts.Jitter)))
			select {
			case <-time.After(d):
			case <-p.stopChan:
				return
			case <-parent.Done():
				return
			}
		}

		if err := p.RunOnce(parent); err != nil {
			p.logger.Warn("探测循环出错", "error", err)
		}

		select {
		case <-time.After(p.opts.Interval):
		case <-p.stopChan:
			return
		case <-parent.Done():
			return
		}
	}
}

// RunOnce 跑一轮探测：拉分组 → worker pool 并发探测 → 落库。
//
// 返回 error 仅在"无法拉分组"等致命错误时；单个 probe.forward 失败不会冒泡，
// 而是作为一行 success=false 写入 group_health_probes。
func (p *Prober) RunOnce(ctx context.Context) error {
	groups, err := p.listGroups(ctx)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}

	p.logger.Debug("health_probe_round_start",
		"groups", len(groups),
		"concurrency", p.opts.Concurrency,
	)

	sem := make(chan struct{}, p.opts.Concurrency)
	var wg sync.WaitGroup
	for _, g := range groups {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.stopChan:
			return nil
		default:
		}

		g := g // capture
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			p.probeAndRecord(ctx, g)
		}()
	}
	wg.Wait()
	return nil
}

// probeAndRecord 对单个分组执行一次 probe.forward 并写一行 group_health_probes。
// probe.forward 永远返回 (*result, nil) —— 失败信息在 result 字段里，不抛 error。
func (p *Prober) probeAndRecord(ctx context.Context, g hostGroup) {
	target := fmt.Sprintf("group:%d", g.ID)
	p.logger.Debug("health_probe_start",
		"target", target,
		sdk.LogFieldGroupID, g.ID,
		sdk.LogFieldPlatform, g.Platform,
	)
	start := time.Now()
	res, err := p.probeForward(ctx, g.ID)
	if err != nil {
		// gRPC 级别错误（极少发生：broker 挂了、context 取消等），
		// 作为一行 error_kind=rpc_error 写入，便于诊断
		p.logger.Warn("health_probe_failed",
			"target", target,
			sdk.LogFieldGroupID, g.ID,
			sdk.LogFieldPlatform, g.Platform,
			"error_kind", "rpc_error",
			sdk.LogFieldError, err,
		)
		_ = p.insertProbeRow(ctx, g, nil, "rpc_error", err.Error())
		p.handleProbeOutcome(ctx, g, false)
		return
	}
	durationMs := time.Since(start).Milliseconds()
	p.logger.Debug("health_probe_completed",
		"target", target,
		sdk.LogFieldGroupID, g.ID,
		sdk.LogFieldPlatform, g.Platform,
		sdk.LogFieldStatus, res.StatusCode,
		sdk.LogFieldDurationMs, durationMs,
		"success", res.Success,
	)
	if !res.Success {
		p.logger.Warn("health_probe_failed",
			"target", target,
			sdk.LogFieldGroupID, g.ID,
			sdk.LogFieldPlatform, g.Platform,
			"error_kind", res.ErrorKind,
			sdk.LogFieldError, res.ErrorMsg,
			sdk.LogFieldStatus, res.StatusCode,
			sdk.LogFieldDurationMs, durationMs,
		)
	}
	if err := p.insertProbeRow(ctx, g, res, "", ""); err != nil {
		p.logger.Error("health_probe_persist_failed",
			"target", target,
			sdk.LogFieldGroupID, g.ID,
			sdk.LogFieldError, err,
		)
	}
	p.handleProbeOutcome(ctx, g, res.Success)
}

// handleProbeOutcome 跟踪连续失败次数 → 触发 unhealthy / recovered 事件。
//
// 仅做日志聚合，不影响探测落库与状态机。计数保存在内存里：
//   - 进程重启后从 0 开始（第一轮探测的恢复事件可能"丢失"，但这只是日志噪声，
//     与持久状态无关）
//   - 多副本部署也各自记自己的计数；这与 prober 本身没有 leader election 一致
const probeUnhealthyThreshold = 3

func (p *Prober) handleProbeOutcome(_ context.Context, g hostGroup, success bool) {
	target := fmt.Sprintf("group:%d", g.ID)
	p.failureMu.Lock()
	defer p.failureMu.Unlock()
	if p.consecutiveFailures == nil {
		p.consecutiveFailures = make(map[int64]int)
	}
	prev := p.consecutiveFailures[g.ID]
	if success {
		if prev >= probeUnhealthyThreshold {
			p.logger.Info("health_target_recovered",
				"target", target,
				sdk.LogFieldGroupID, g.ID,
				sdk.LogFieldPlatform, g.Platform,
			)
		}
		delete(p.consecutiveFailures, g.ID)
		return
	}
	cur := prev + 1
	p.consecutiveFailures[g.ID] = cur
	if cur == probeUnhealthyThreshold {
		p.logger.Error("health_target_unhealthy",
			"target", target,
			sdk.LogFieldGroupID, g.ID,
			sdk.LogFieldPlatform, g.Platform,
			"consecutive_failures", cur,
		)
	}
}

// insertProbeRow 往 group_health_probes 写一行。
// res 可以为 nil（RPC 错误时），此时 errKind/errMsg 由调用方提供。
func (p *Prober) insertProbeRow(ctx context.Context, g hostGroup, res *hostProbeForwardResult, overrideKind, overrideMsg string) error {
	var (
		success    bool
		latencyMs  int64
		statusCode int64
		accountID  int64
		model      string
		errKind    = overrideKind
		errMsg     = overrideMsg
	)
	if res != nil {
		success = res.Success
		latencyMs = res.LatencyMs
		statusCode = res.StatusCode
		accountID = res.AccountID
		model = res.Model
		if errKind == "" {
			errKind = res.ErrorKind
		}
		if errMsg == "" {
			errMsg = res.ErrorMsg
		}
	}
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO group_health_probes
			(group_id, platform, probed_at, success, latency_ms, status_code, account_id, model, error_kind, error_msg)
		VALUES ($1, $2, NOW(), $3, $4, $5, $6, $7, $8, $9)
	`, g.ID, g.Platform, success, latencyMs, statusCode, accountID, model, errKind, errMsg)
	return err
}

// ============================================================================
// 手动探测（前端"立即探测"按钮）
// ============================================================================

// GroupProbeResult 一次手动分组探测的结果，给前端展示。
//
// 与 Step 1 之前的版本相比字段简化：分组级探测每次只发一次请求，
// 不再有 Total/Success/Failed 的聚合概念。保留 DurationMS 让前端展示耗时。
type GroupProbeResult struct {
	GroupID    int64  `json:"group_id"`
	Success    bool   `json:"success"`
	LatencyMS  int64  `json:"latency_ms"`
	AccountID  int64  `json:"account_id,omitempty"` // 本次命中的账号（运维诊断）
	Model      string `json:"model,omitempty"`      // 探测用的 model
	ErrorKind  string `json:"error_kind,omitempty"`
	ErrorMsg   string `json:"error_msg,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// ProbeGroup 手动探测单个分组；admin 路由触发。
// 结果会落到 group_health_probes 表，然后返回结构体给前端展示 toast。
func (p *Prober) ProbeGroup(ctx context.Context, groupID int64) (GroupProbeResult, error) {
	if p.host == nil {
		return GroupProbeResult{}, &HostNotReadyError{}
	}

	// 先确认这个 group 存在（通过 groups.list 拿 platform，顺便校验）
	groups, err := p.listGroups(ctx)
	if err != nil {
		return GroupProbeResult{}, err
	}
	var target *hostGroup
	for i := range groups {
		if groups[i].ID == groupID {
			target = &groups[i]
			break
		}
	}
	if target == nil {
		return GroupProbeResult{}, &GroupNotFoundError{ID: groupID}
	}

	manualTarget := fmt.Sprintf("group:%d", groupID)
	p.logger.Debug("health_probe_start",
		"target", manualTarget,
		sdk.LogFieldGroupID, groupID,
		sdk.LogFieldPlatform, target.Platform,
		"manual", true,
	)
	start := time.Now()
	res, err := p.probeForward(ctx, groupID)
	if err != nil {
		p.logger.Warn("health_probe_failed",
			"target", manualTarget,
			sdk.LogFieldGroupID, groupID,
			sdk.LogFieldPlatform, target.Platform,
			"error_kind", "rpc_error",
			"manual", true,
			sdk.LogFieldError, err,
		)
		_ = p.insertProbeRow(ctx, *target, nil, "rpc_error", err.Error())
		return GroupProbeResult{}, err
	}
	p.logger.Debug("health_probe_completed",
		"target", manualTarget,
		sdk.LogFieldGroupID, groupID,
		sdk.LogFieldPlatform, target.Platform,
		sdk.LogFieldStatus, res.StatusCode,
		sdk.LogFieldDurationMs, time.Since(start).Milliseconds(),
		"success", res.Success,
		"manual", true,
	)
	_ = p.insertProbeRow(ctx, *target, res, "", "")

	return GroupProbeResult{
		GroupID:    groupID,
		Success:    res.Success,
		LatencyMS:  res.LatencyMs,
		AccountID:  res.AccountID,
		Model:      res.Model,
		ErrorKind:  res.ErrorKind,
		ErrorMsg:   res.ErrorMsg,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

// GroupNotFoundError 分组探测时分组不存在。
type GroupNotFoundError struct{ ID int64 }

func (e *GroupNotFoundError) Error() string {
	return "分组不存在"
}

// HostNotReadyError 插件还没拿到 host service（通常是 Core 太老 / 未启用 HostService）。
type HostNotReadyError struct{}

func (e *HostNotReadyError) Error() string {
	return "HostService 未就绪：core 版本过旧或 host service 未注入"
}
