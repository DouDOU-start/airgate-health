# airgate-health — Claude 开发指南

> 叠加在 monorepo 根 `../CLAUDE.md` 之上。完整流程见共享 skill **`develop-plugin`**；接口契约见 `../airgate-sdk/CLAUDE.md`。

- **插件身份**：id `airgate-health`，type `extension`，作用 = 提供商健康监控。
- 实现 `sdk.ExtensionPlugin`：后台周期任务探测上游健康（`prober.go`），聚合结果（`aggregator.go`），经 `Host.Invoke` 回写 core。Core 的 `/status` 路由反代至本插件提供的公开状态页。

## 🚫 红线

通用边界铁律（只依赖 `airgate-sdk`、经 `Host.Invoke`/`InvokeStream` 调 core、`plugin.yaml` 由 `make manifest` 生成不可手改、前端单 `index.js` bundle）见 skill **`develop-plugin`「🚫 边界铁律」**。本仓特有：

- `db_dsn` 连接仅读写插件自有表 `group_health_probes`；分组元信息与状态页可见性过滤一律经 `Host.Invoke("groups.list")`（支持 `{public_only, user_id}`），**禁止直接查 core 的 `groups`/`user_allowed_groups` 表**。

## 命令

构建/发布命令见 skill **`develop-plugin`「构建 / 发布」**；本仓实际 make 目标以 `Makefile` 为准。
