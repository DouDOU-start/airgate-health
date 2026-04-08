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
var PluginVersion = "0.1.0"

// BuildPluginInfo 构造插件元信息（PluginInfo）。
//
// FrontendPages 都是管理员级（普通用户不需要看运维健康面板）；
// 公开状态页通过 core 的 /status 路由直接静态托管，不在 FrontendPages 里登记
// （FrontendPages 是注册到 admin/user 菜单的，不适合 public 页面）。
//
// ConfigSchema 涵盖：core 回调地址 + 管理员 API key + 探测周期/并发/超时/保留期 + public 开关。
// db_dsn 不在这里声明，由 core 自动注入。
func BuildPluginInfo() sdk.PluginInfo {
	return sdk.PluginInfo{
		ID:          PluginID,
		Name:        PluginName,
		Version:     PluginVersion,
		SDKVersion:  sdk.SDKVersion,
		Description: "AI 提供商健康监控：主动探测、可用率/延迟聚合、对外公开状态页",
		Author:      "AirGate",
		Type:        sdk.PluginTypeExtension,

		FrontendPages: []sdk.FrontendPage{
			{
				Path:        "/admin/health",
				Title:       "健康监控",
				Icon:        "activity",
				Description: "账号可用率、延迟趋势、主动探测面板",
				Audience:    "admin",
			},
		},

		ConfigSchema: []sdk.ConfigField{
			// 注意：以下字段由 core 自动注入，插件无需声明也无需管理员填写：
			//   - db_dsn        ← core 数据库配置
			//   - core_base_url ← core 自身 HTTP 监听地址（http://127.0.0.1:<port>）
			//   - admin_api_key ← 解密自 settings.admin_api_key_encrypted
			//                     前提：管理员已在「系统设置 → 安全与认证」生成过 admin key

			// === 探测参数 ===
			{Key: "probe_interval_seconds", Label: "探测间隔（秒）", Type: "int", Default: "60", Description: "周期性探测的最小间隔；过小会浪费配额"},
			{Key: "probe_concurrency", Label: "探测并发数", Type: "int", Default: "8", Description: "同时进行的探测请求上限"},
			{Key: "probe_timeout_seconds", Label: "单次探测超时（秒）", Type: "int", Default: "15", Description: "单个 TestAccount 调用的超时上限"},

			// === 数据保留 ===
			{Key: "retention_days", Label: "原始数据保留天数", Type: "int", Default: "30", Description: "health_probes 表的清理周期；聚合查询足以覆盖 7/15/30 天窗口"},

			// === 公开状态页 ===
			{Key: "public_status_enabled", Label: "启用公开状态页", Type: "bool", Default: "true", Description: "开启后 /status 路由对外公开（无需登录），仅暴露脱敏的平台维度聚合"},
		},
	}
}
