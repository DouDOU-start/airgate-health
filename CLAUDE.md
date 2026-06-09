# airgate-health — Claude 开发指南

> 叠加在 monorepo 根 `../CLAUDE.md` 之上。完整流程见共享 skill **`develop-plugin`**；接口契约见 `../airgate-sdk/CLAUDE.md`。

- **插件身份**：id `airgate-health`，type `extension`，作用 = 提供商健康监控。
- 实现 `sdk.ExtensionPlugin`：后台周期任务探测上游健康，结果经 `Host.Invoke` 回写/通知 core。

## 🚫 红线

- 只依赖 `airgate-sdk`，禁止 import core 内部；用 core 能力经 `Host.Invoke`/`InvokeStream`。
- `plugin.yaml` 由 `make manifest` 生成，不可手改。
- 前端单 `index.js` → `web/dist/index.js`，用 `@doudou-start/airgate-theme`。

## 命令

`make dev`（独立调试）· `make manifest` · `make build` · `make ci` · `make release`
