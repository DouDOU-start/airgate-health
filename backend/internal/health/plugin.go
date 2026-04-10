package health

import (
	"context"
	"database/sql"
	"log/slog"
	"sync/atomic"
	"time"

	sdk "github.com/DouDOU-start/airgate-sdk"
)

// Plugin 是健康监控插件主体，实现 sdk.ExtensionPlugin。
//
// 生命周期：
//  1. core 启动 → grpc Init() → Plugin.Init(ctx)：读 db_dsn、打开 core DB，
//     通过 sdk.HostAware 从 PluginContext 拿到 Core 反向调用客户端（Host）。
//  2. core 调 Migrate() → Plugin.Migrate()：建插件自有表 group_health_probes，
//     同时 DROP 掉旧版 health_probes / health_settings。
//  3. core 调 Start() → Plugin.Start()：启动 prober 主循环 + 背景清理任务。
//  4. core 调 RegisterRoutes() → 注册 admin / public HTTP handler。
//  5. core 关闭 → Plugin.Stop()：停 prober，关 DB。
//
// 软失败原则：配置不全时插件依然加载成功，所有数据接口返回 503。
// 与旧版本的关键差异：
//   - 不再需要 admin_api_key 配置（通过 HostService 回调 core，无需 HTTP 鉴权）
//   - 不再需要 core_base_url 配置（同上）
//   - prober 探测粒度从账号级降到分组级
type Plugin struct {
	logger *slog.Logger
	ctx    sdk.PluginContext

	db     *sql.DB
	host   sdk.Host
	agg    *Aggregator
	prober *Prober

	retentionDays int
	publicEnabled atomic.Bool

	// purgeStop 用于停止后台清理协程
	purgeStop chan struct{}
	purgeDone chan struct{}
}

var _ sdk.ExtensionPlugin = (*Plugin)(nil)

// New 构造 Plugin 实例
func New() *Plugin {
	return &Plugin{}
}

// Info 返回插件元信息
func (p *Plugin) Info() sdk.PluginInfo {
	return BuildPluginInfo()
}

// Init 由 core 调用，注入运行时上下文。
//
// 这里完成所有"基于配置的资源构建"：DB 连接、Aggregator、Prober。
// 软失败：如果 db_dsn 缺失或 host 不可用，插件不返回 error 而是把 prober 留空，
// 让管理员能在 UI 看到插件并填配置。
func (p *Plugin) Init(ctx sdk.PluginContext) error {
	p.ctx = ctx
	if ctx != nil {
		p.logger = ctx.Logger()
	}
	if p.logger == nil {
		p.logger = slog.Default()
	}

	cfg := ctx.Config()
	if cfg == nil {
		p.logger.Warn("plugin config 为空，插件以未配置态加载；请在后台填写配置后 Reload")
		return nil
	}

	dsn := cfg.GetString("db_dsn")
	if dsn == "" {
		p.logger.Warn("db_dsn 未配置，插件以未配置态加载；请在后台填写后 Reload")
		return nil
	}
	db, err := openDB(dsn)
	if err != nil {
		p.logger.Warn("打开 core 数据库失败，插件以未配置态加载",
			"error", err,
			"hint", "请检查 db_dsn 后在插件管理页 Reload",
		)
		return nil
	}
	p.db = db
	p.agg = NewAggregator(db)

	// 通过 sdk.HostAware 拿到 core 反向调用客户端。
	// 旧版本走 HTTP + admin_api_key；现在走 hashicorp/go-plugin GRPCBroker。
	if hostAware, ok := ctx.(sdk.HostAware); ok {
		p.host = hostAware.Host()
	}
	if p.host == nil {
		p.logger.Warn("HostService 不可用（core 版本过旧或 host 未注入），探测循环不会启动；aggregator 仍可读历史数据")
		// aggregator 仍然可用（读取已有数据），但 prober 不启动
		p.publicEnabled.Store(cfg.GetBool("public_status_enabled"))
		return nil
	}

	intervalSec := cfg.GetInt("probe_interval_seconds")
	if intervalSec <= 0 {
		intervalSec = 300 // 默认 5 分钟，远低于旧版 60s——分组级探测每次真的烧 token
	}
	concurrency := cfg.GetInt("probe_concurrency")
	if concurrency <= 0 {
		concurrency = 4 // 默认 4，分组数远少于账号数
	}
	p.prober = NewProber(p.logger, db, p.host, ProberOptions{
		Interval:    time.Duration(intervalSec) * time.Second,
		Concurrency: concurrency,
		Jitter:      5 * time.Second,
	})

	p.retentionDays = cfg.GetInt("retention_days")
	if p.retentionDays <= 0 {
		p.retentionDays = 30
	}

	// public 开关默认开启
	publicEnabled := true
	if v := cfg.GetString("public_status_enabled"); v == "false" || v == "0" {
		publicEnabled = false
	}
	p.publicEnabled.Store(publicEnabled)

	p.logger.Info("健康监控插件初始化完成",
		"interval_seconds", intervalSec,
		"concurrency", concurrency,
		"retention_days", p.retentionDays,
		"public_enabled", publicEnabled,
	)
	return nil
}

// Start 启动 prober 主循环 + 后台数据清理协程。
func (p *Plugin) Start(_ context.Context) error {
	if p.prober != nil {
		// 用 background ctx，不依赖 core 传进来的可能短命的 ctx
		p.prober.Start(context.Background())
	}
	if p.db != nil && p.retentionDays > 0 {
		p.startPurgeLoop()
	}
	p.logger.Info("健康监控插件启动")
	return nil
}

// Stop 停止 prober 与清理协程，关 DB。
func (p *Plugin) Stop(_ context.Context) error {
	if p.prober != nil {
		p.prober.Stop()
	}
	if p.purgeStop != nil {
		close(p.purgeStop)
		<-p.purgeDone
		p.purgeStop = nil
	}
	if p.db != nil {
		_ = p.db.Close()
	}
	p.logger.Info("健康监控插件停止")
	return nil
}

// RegisterRoutes 注册 HTTP 路由
func (p *Plugin) RegisterRoutes(r sdk.RouteRegistrar) {
	p.registerRoutes(r)
}

// Migrate 创建插件自有表。
// 未配置 db_dsn 时跳过，让插件能继续加载到 UI 让管理员填配置。
func (p *Plugin) Migrate() error {
	if p.db == nil {
		p.logger.Warn("Migrate 跳过：db 未初始化（db_dsn 未配置）")
		return nil
	}
	if err := migrate(p.db); err != nil {
		return err
	}
	p.logger.Info("健康监控插件自有表迁移完成",
		"tables", []string{"group_health_probes"},
		"dropped", []string{"health_probes", "health_settings"},
	)
	return nil
}

// BackgroundTasks 当前不声明 core 调度的后台任务。
//
// 我们的探测循环和清理循环都在插件进程内自起协程；这是因为：
//   - 探测的间隔远小于 core BackgroundTasks 的常规调度粒度
//   - 清理逻辑只需要每天跑一次，自起 ticker 比让 core 跨进程调度更简单
//   - 保持 BackgroundTasks 为空让插件对 core 的依赖面更小
func (p *Plugin) BackgroundTasks() []sdk.BackgroundTask {
	return nil
}

// Configured 报告插件是否已可服务请求（aggregator 已就绪）。
// 注意：允许 host 为 nil 时 aggregator 仍可用，这样管理员即使在旧版 core 上
// 也能看到历史数据。
func (p *Plugin) Configured() bool {
	return p.db != nil && p.agg != nil
}

// startPurgeLoop 启动后台清理协程，每 24 小时跑一次 purgeOldProbes。
func (p *Plugin) startPurgeLoop() {
	p.purgeStop = make(chan struct{})
	p.purgeDone = make(chan struct{})
	go func() {
		defer close(p.purgeDone)
		// 启动后立即跑一次（避免插件刚装好时旧数据堆积）
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if n, err := purgeOldProbes(ctx, p.db, p.retentionDays); err == nil && n > 0 {
			p.logger.Info("清理过期 probe", "deleted", n)
		}
		cancel()

		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-p.purgeStop:
				return
			case <-t.C:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				if n, err := purgeOldProbes(ctx, p.db, p.retentionDays); err == nil && n > 0 {
					p.logger.Info("清理过期 probe", "deleted", n)
				} else if err != nil {
					p.logger.Warn("清理过期 probe 失败", "error", err)
				}
				cancel()
			}
		}
	}()
}

// OnConfigUpdate 实现 sdk.ConfigWatcher：core 调用 Reload 时透传新配置。
//
// 简化处理：直接重新走一遍 Init 流程。Stop 现有 prober → 重新 Init → Start。
func (p *Plugin) OnConfigUpdate(_ sdk.PluginConfig) error {
	if p.prober != nil {
		p.prober.Stop()
		p.prober = nil
	}
	if p.purgeStop != nil {
		close(p.purgeStop)
		<-p.purgeDone
		p.purgeStop = nil
	}
	if p.db != nil {
		_ = p.db.Close()
		p.db = nil
		p.agg = nil
	}
	p.host = nil
	if err := p.Init(p.ctx); err != nil {
		return err
	}
	if err := p.Migrate(); err != nil {
		return err
	}
	return p.Start(context.Background())
}

