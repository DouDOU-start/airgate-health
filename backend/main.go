// Package main 是 airgate-health 插件的入口。
//
// airgate-health 是 airgate-core 的健康监控插件：
//   - 周期性主动探测 core 中所有启用账号的连通性（通过回调 core 的 TestAccount 接口）
//   - 写入插件自有 health_probes 时序表
//   - 提供 admin API 暴露聚合后的可用率/延迟数据，前端渲染管理面板
//   - 提供 public /status API/页面，展示脱敏的平台维度可用率（无需登录）
//   - 支持维护模式与公告横幅
//
// 与 epay 一样，本插件通过 core 自动注入的 db_dsn 复用 core 的 PostgreSQL，
// 自有表 (health_probes / health_settings) 与 core 表共用同一个连接，但本插件
// 不写 core 表（账号状态机仍由 core scheduler 自己根据真实流量维护，避免双写）。
package main

import (
	sdkgrpc "github.com/DouDOU-start/airgate-sdk/grpc"

	"github.com/DouDOU-start/airgate-health/backend/internal/health"
)

func main() {
	sdkgrpc.Serve(health.New())
}
