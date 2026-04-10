package health

import (
	"context"
	"database/sql"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	sdk "github.com/DouDOU-start/airgate-sdk"
)

// prober.go：分组级主动探测器（Step 2 重写）。
//
// 工作循环：
//  1. 通过 sdk.Host.ListGroups() 拿到 core 当前所有分组
//  2. 用 worker pool（受 concurrency 限制）对每个分组调 sdk.Host.ProbeForward
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

type ProberOptions struct {
	Interval    time.Duration // 主循环间隔；默认 300s（5 分钟）
	Concurrency int           // 同时进行的 ProbeForward 上限；默认 4
	Jitter      time.Duration // 每次循环开头随机等待 [0, Jitter)；默认 5s
}

func defaultProberOptions() ProberOptions {
	return ProberOptions{
		Interval:    5 * time.Minute,
		Concurrency: 4,
		Jitter:      5 * time.Second,
	}
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
// 返回 error 仅在"无法拉分组"等致命错误时；单个 ProbeForward 失败不会冒泡，
// 而是作为一行 success=false 写入 group_health_probes。
func (p *Prober) RunOnce(ctx context.Context) error {
	groups, err := p.host.ListGroups(ctx)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}

	p.logger.Debug("开始本轮分组探测", "groups", len(groups), "concurrency", p.opts.Concurrency)

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

// probeAndRecord 对单个分组执行一次 ProbeForward 并写一行 group_health_probes。
// ProbeForward 永远返回 (*result, nil) —— 失败信息在 result 字段里，不抛 error。
func (p *Prober) probeAndRecord(ctx context.Context, g sdk.HostGroup) {
	res, err := p.host.ProbeForward(ctx, sdk.HostProbeForwardRequest{GroupID: g.ID})
	if err != nil {
		// gRPC 级别错误（极少发生：broker 挂了、context 取消等），
		// 作为一行 error_kind=rpc_error 写入，便于诊断
		p.logger.Warn("ProbeForward RPC 失败", "group_id", g.ID, "error", err)
		_ = p.insertProbeRow(ctx, g, nil, "rpc_error", err.Error())
		return
	}
	if err := p.insertProbeRow(ctx, g, res, "", ""); err != nil {
		p.logger.Warn("写入 probe 失败", "group_id", g.ID, "error", err)
	}
}

// insertProbeRow 往 group_health_probes 写一行。
// res 可以为 nil（RPC 错误时），此时 errKind/errMsg 由调用方提供。
func (p *Prober) insertProbeRow(ctx context.Context, g sdk.HostGroup, res *sdk.HostProbeForwardResult, overrideKind, overrideMsg string) error {
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

	// 先确认这个 group 存在（通过 ListGroups 拿 platform，顺便校验）
	groups, err := p.host.ListGroups(ctx)
	if err != nil {
		return GroupProbeResult{}, err
	}
	var target *sdk.HostGroup
	for i := range groups {
		if groups[i].ID == groupID {
			target = &groups[i]
			break
		}
	}
	if target == nil {
		return GroupProbeResult{}, &GroupNotFoundError{ID: groupID}
	}

	start := time.Now()
	res, err := p.host.ProbeForward(ctx, sdk.HostProbeForwardRequest{GroupID: groupID})
	if err != nil {
		_ = p.insertProbeRow(ctx, *target, nil, "rpc_error", err.Error())
		return GroupProbeResult{}, err
	}
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
