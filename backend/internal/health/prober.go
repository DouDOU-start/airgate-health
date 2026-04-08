package health

import (
	"context"
	"database/sql"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// prober.go：周期性主动探测器。
//
// 工作循环：
//  1. 从 core accounts 表拉所有 status != 'disabled' 的账号（按 platform 分桶）。
//  2. 用 worker pool（受 concurrency 限制）对每个账号调用 CoreClient.TestAccount。
//  3. 把结果写一行到 health_probes。
//  4. 抖动避免雪崩。
//
// 不在这里实现 leader election：与 airgate 当前部署一致（单实例假设）。
// 多副本部署如果部署多份本插件，会出现重复探测——但因为表只是 append，
// 数据正确性不受影响，只是浪费上游配额。如果未来需要可在这里加 SET-NX。

type ProberOptions struct {
	Interval    time.Duration // 主循环间隔；默认 60s
	Concurrency int           // 同时进行的 TestAccount 调用上限；默认 8
	Jitter      time.Duration // 在每次循环开头随机等待 [0, Jitter)，避免和外部 cron 对齐；默认 5s
}

func defaultProberOptions() ProberOptions {
	return ProberOptions{
		Interval:    60 * time.Second,
		Concurrency: 8,
		Jitter:      5 * time.Second,
	}
}

type Prober struct {
	logger *slog.Logger
	db     *sql.DB
	client *CoreClient
	opts   ProberOptions

	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
	doneChan chan struct{}
}

func NewProber(logger *slog.Logger, db *sql.DB, client *CoreClient, opts ProberOptions) *Prober {
	if opts.Interval <= 0 {
		opts.Interval = 60 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if opts.Jitter < 0 {
		opts.Jitter = 5 * time.Second
	}
	return &Prober{
		logger: logger.With("component", "prober"),
		db:     db,
		client: client,
		opts:   opts,
	}
}

// Start 启动探测主循环。可重复调用，已运行时是 no-op。
func (p *Prober) Start(parent context.Context) {
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

	// 启动后短暂延迟一次，避免 core 还在装载其它插件时立刻打 TestAccount
	select {
	case <-time.After(2 * time.Second):
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

// targetAccount 探测目标的最小视图。
type targetAccount struct {
	ID       int64
	Platform string
}

// RunOnce 跑一轮探测：拉账号 → worker pool 并发探测 → 落库。
//
// 返回 error 仅在"无法拉账号"等致命错误时；单个 TestAccount 失败不会冒泡，
// 而是作为一行 success=false 写入 health_probes。
func (p *Prober) RunOnce(ctx context.Context) error {
	targets, err := p.loadTargets(ctx)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}

	p.logger.Debug("开始本轮探测", "targets", len(targets), "concurrency", p.opts.Concurrency)

	// 3. worker pool
	sem := make(chan struct{}, p.opts.Concurrency)
	var wg sync.WaitGroup
	for _, t := range targets {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.stopChan:
			return nil
		default:
		}

		t := t // capture
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			res := p.client.TestAccount(ctx, t.ID)
			if err := p.recordProbe(ctx, t, res); err != nil {
				p.logger.Warn("写入 probe 失败", "account_id", t.ID, "error", err)
			}
		}()
	}
	wg.Wait()
	return nil
}

// loadTargets 从 core accounts 表拉取所有需要探测的账号。
// status='disabled' 的账号被排除（管理员明确关掉的）；
// 其余包括 active 和 error 状态——保留 error 是为了能检测到恢复。
func (p *Prober) loadTargets(ctx context.Context) ([]targetAccount, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT id, platform FROM accounts WHERE status != 'disabled'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []targetAccount
	for rows.Next() {
		var t targetAccount
		if err := rows.Scan(&t.ID, &t.Platform); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// recordProbe 把 ProbeResult 落到 health_probes 表（一行）。
func (p *Prober) recordProbe(ctx context.Context, t targetAccount, r ProbeResult) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO health_probes
			(account_id, platform, probed_at, success, latency_ms, status_code, error_kind, error_msg)
		VALUES ($1, $2, NOW(), $3, $4, $5, $6, $7)
	`, t.ID, t.Platform, r.Success, r.LatencyMS, r.StatusCode, r.ErrorKind, r.ErrorMsg)
	return err
}

// GroupProbeResult 一次分组探测的聚合结果，给前端展示。
type GroupProbeResult struct {
	GroupID    int64 `json:"group_id"`
	Total      int   `json:"total"`       // 实际探测的账号数
	Success    int   `json:"success"`     // 成功数
	Failed     int   `json:"failed"`      // 失败数
	DurationMS int64 `json:"duration_ms"` // 整轮耗时
}

// ProbeGroup 手动探测一个分组下所有非 disabled 账号；admin 路由触发。
// 并发受 prober 全局 concurrency 限制。每个账号的结果会落到 health_probes 表，
// 然后返回汇总给前端展示一条 toast。
func (p *Prober) ProbeGroup(ctx context.Context, groupID int64) (GroupProbeResult, error) {
	// 校验分组存在
	var exists bool
	if err := p.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM groups WHERE id = $1)`, groupID,
	).Scan(&exists); err != nil {
		return GroupProbeResult{}, err
	}
	if !exists {
		return GroupProbeResult{}, &GroupNotFoundError{ID: groupID}
	}

	// 拉取分组下所有非 disabled 账号
	rows, err := p.db.QueryContext(ctx, `
		SELECT a.id, a.platform
		FROM accounts a
		JOIN account_groups ag ON ag.account_id = a.id
		WHERE ag.group_id = $1 AND a.status != 'disabled'
	`, groupID)
	if err != nil {
		return GroupProbeResult{}, err
	}
	defer rows.Close()
	var targets []targetAccount
	for rows.Next() {
		var t targetAccount
		if err := rows.Scan(&t.ID, &t.Platform); err != nil {
			return GroupProbeResult{}, err
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return GroupProbeResult{}, err
	}

	start := time.Now()
	out := GroupProbeResult{GroupID: groupID, Total: len(targets)}
	if len(targets) == 0 {
		out.DurationMS = time.Since(start).Milliseconds()
		return out, nil
	}

	sem := make(chan struct{}, p.opts.Concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, t := range targets {
		t := t
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			res := p.client.TestAccount(ctx, t.ID)
			if err := p.recordProbe(ctx, t, res); err != nil {
				p.logger.Warn("写入 probe 失败", "account_id", t.ID, "error", err)
			}
			mu.Lock()
			if res.Success {
				out.Success++
			} else {
				out.Failed++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	out.DurationMS = time.Since(start).Milliseconds()
	return out, nil
}

// GroupNotFoundError 分组探测时分组不存在。
type GroupNotFoundError struct{ ID int64 }

func (e *GroupNotFoundError) Error() string {
	return "分组不存在"
}
