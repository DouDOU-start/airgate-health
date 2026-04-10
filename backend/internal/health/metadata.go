package health

import (
	sdk "github.com/DouDOU-start/airgate-sdk"
)

const (
	// PluginID 插件唯一标识，与 core marketplace 注册项一致；
	// public 状态页路由也以这个 ID 转发到本插件，所以**改名要同步改 core router**。
	PluginID = "airgate-health"
	// PluginName 显示名称
	PluginName = "健康监控插件"
)

// PluginVersion 插件版本号。
//
// 这里是 var 而不是 const，是为了让 release CI 通过 ldflags 把 git tag 注入进来：
//
//	go build -ldflags "-X 'github.com/DouDOU-start/airgate-health/backend/internal/health.PluginVersion=0.1.0'"
//
// 默认值仅用于本地开发；正式发版的版本号永远来自 git tag（去掉 v 前缀）。
var PluginVersion = "0.2.0"

// BuildPluginInfo 构造插件元信息（PluginInfo）。
//
// Step 2 重写后的关键变化：
//   - 描述改为"分组级黑盒探测"
//   - ConfigSchema 删除 probe_timeout_seconds（ProbeForward 由 core 内部管理超时）
//   - 默认 interval 从 60s 改为 300s（5 分钟），默认 concurrency 从 8 改为 4
//   - 不再声明 admin_api_key / core_base_url（走 HostService 不需要）
//   - db_dsn 由 core 自动注入，也不在 schema 里显式声明
func BuildPluginInfo() sdk.PluginInfo {
	return sdk.PluginInfo{
		ID:          PluginID,
		Name:        PluginName,
		Version:     PluginVersion,
		SDKVersion:  sdk.SDKVersion,
		Description: "AI 提供商健康监控：分组级黑盒探测、可用率/延迟聚合、对外公开状态页",
		Author:      "AirGate",
		Type:        sdk.PluginTypeExtension,
		// 健康监控插件的核心能力是分组级黑盒探测，必须显式声明：
		//   - host.list_groups   —— prober.RunOnce 遍历分组目标
		//   - host.probe_forward —— 真正的探测请求执行
		// 没声明任何一项都会被 core 的 capability interceptor 以 PermissionDenied 拒绝。
		// 注：账号状态机反馈由 core 在 ProbeForward 内部完成，prober 不直接调
		// host.report_account_result，所以这里不声明该 capability。
		Capabilities: []sdk.Capability{
			sdk.CapabilityHostListGroups,
			sdk.CapabilityHostProbeForward,
		},

		// 不再声明独立的 admin 前端页面：分组级可用率会由 core 的分组管理页直接展示，
		// 健康监控插件只保留后端 prober + group_health_probes 表 + admin API 作为数据源。
		// 删除时机：确认 admin 健康监控 tab 不再被任何运维流程依赖（2026-04-10）。
		FrontendPages: nil,

		ConfigSchema: []sdk.ConfigField{
			// 注意：db_dsn 由 core 自动注入，插件无需声明也无需管理员填写。
			// admin_api_key 和 core_base_url 已下线——插件通过 HostService 反向调用 core，
			// 不再需要 HTTP + Bearer 鉴权。

			// === 探测参数 ===
			{Key: "probe_interval_seconds", Label: "探测间隔（秒）", Type: "int", Default: "300", Description: "周期性分组探测的最小间隔；每次探测都是真实的上游 API 请求，过小会浪费上游配额"},
			{Key: "probe_concurrency", Label: "探测并发数", Type: "int", Default: "4", Description: "同时进行的分组探测上限"},

			// === 数据保留 ===
			{Key: "retention_days", Label: "原始数据保留天数", Type: "int", Default: "30", Description: "group_health_probes 表的清理周期；聚合查询足以覆盖 7/15/30 天窗口"},

			// === 公开状态页 ===
			{Key: "public_status_enabled", Label: "启用公开状态页", Type: "bool", Default: "true", Description: "开启后 /status 路由对外公开（无需登录），暴露脱敏的分组维度聚合"},
		},
	}
}
