# airgate-health

AirGate 的健康监控扩展插件：主动探测 AI 提供商账号、聚合可用率与延迟指标、对外提供脱敏的公开状态页。

运行时元信息以 `backend/internal/health/metadata.go` 为单源，根目录 `plugin.yaml` 由 `genmanifest` 自动生成，不要手工修改。

## 功能

- 周期性主动探测所有已配置的 AI 账号（通过 SDK `TestAccount`）
- 按账号 / 分组 / 平台维度聚合可用率与延迟（7 / 15 / 30 / 90 天窗口）
- 管理员面板：总览、账号列表、单账号 90 天日桶详情、分组聚合、手动触发整组探测
- 公开状态页（`/status`）：脱敏的分组维度可用率 + 90 天方格图，可通过开关一键关闭
- 数据保留期可配置，过期原始探测记录自动清理

## 路由

插件路径已被 core 的 `ExtensionProxy` 剥掉前缀，下表展示的是**外部访问路径**。

### 管理员入口（`/api/v1/ext/airgate-health/*`，需要 admin 角色）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/admin/overview`              | 平台 + 账号 总览 |
| GET  | `/admin/accounts`              | 账号摘要列表（`?platform=&window=`）|
| GET  | `/admin/accounts/{id}`         | 单账号详情 + 90 天日桶 |
| GET  | `/admin/groups`                | group 维度聚合 |
| POST | `/admin/probe/group/{id}`      | 手动触发一次整组探测 |

### 公开入口（`/status/*`，无需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/status`                       | 静态 HTML 状态页 |
| GET  | `/status/api/summary`           | 脱敏的分组维度聚合（不含 `account_id` / `error_msg`） |
| GET  | `/status/assets/*`              | 状态页静态资源 |

公开入口受 `public_status_enabled` 开关控制；关闭后直接返回 404。

## 配置

`db_dsn` / `core_base_url` / `admin_api_key` 由 core 自动注入，**管理员无需填写**。前提是已经在「系统设置 → 安全与认证」生成过 admin API key，然后在「插件管理」热加载本插件。

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `probe_interval_seconds` | int  | 60   | 周期性探测的最小间隔；过小会浪费配额 |
| `probe_concurrency`      | int  | 8    | 同时进行的探测请求上限 |
| `probe_timeout_seconds`  | int  | 15   | 单次 `TestAccount` 调用的超时上限 |
| `retention_days`         | int  | 30   | `health_probes` 表的清理周期 |
| `public_status_enabled`  | bool | true | 开启后 `/status` 路由对外公开 |

## 目录结构

```
├── backend/                        Go 后端（插件主体）
│   ├── main.go                      gRPC 插件入口
│   ├── cmd/
│   │   └── genmanifest/             plugin.yaml 生成器
│   └── internal/health/
│       ├── metadata.go              插件元信息（单源）
│       ├── plugin.go                Plugin 主结构 / 生命周期
│       ├── routes.go                admin + public 路由注册与中间件
│       ├── prober.go                周期性探测调度器
│       ├── aggregator.go            可用率 / 延迟聚合查询
│       ├── db.go                    health_probes 表 schema 与读写
│       ├── core_client.go           回调 core 拉取账号列表 / 触发 TestAccount
│       ├── assets.go                webdist 静态资源嵌入
│       └── webdist/                 前端构建产物（go:embed）
├── web/                            前端
│   ├── src/                         管理员面板（/admin/health）
│   ├── status.html                  独立打包的公开状态页
│   ├── vite.config.ts               admin 面板构建配置
│   └── vite.status.config.ts        status.html 构建配置
├── plugin.yaml                     插件描述文件（生成产物）
└── Makefile                        构建脚本
```

## 规则

- `metadata.go` 是运行时真相
- `plugin.yaml` 是生成产物（`make manifest`）
- 改 `PluginID` 必须同步改 core router 的 `/status/*` 转发目标
- public 接口必须脱敏，不暴露 `account_id` / `error_msg`

## 构建

```bash
make install        # 安装 web 依赖与 Go 模块
make build          # 完整构建：web/dist → backend/webdist → bin/airgate-health
make manifest       # 重新生成 plugin.yaml
make test           # 运行后端测试
make vet            # 静态分析
make release        # 编译 Linux amd64 发布二进制
```

开发时把本插件以 dev 模式挂到 core：

```yaml
# airgate-core/backend/config.yaml
plugins:
  dev:
    - name: airgate-health
      path: /absolute/path/to/airgate-health/backend
```

然后 `cd airgate-core/backend && go run ./cmd/server` 即可。

## 发版

`metadata.go` 中的 `PluginVersion` 是 `var`，默认值用于本地开发。**正式发版只需要打 git tag，不要手工改版本号字段**：

```bash
git tag v0.2.0
git push origin v0.2.0
```

GitHub Actions 的 `release.yml` 工作流会自动：

1. 矩阵构建 4 个平台二进制（linux/darwin × amd64/arm64）
2. 通过 `-ldflags "-X .../health.PluginVersion=${version}"` 把 git tag（去掉 `v` 前缀）注入到二进制
3. 上传到 GitHub Release，资产命名 `airgate-health-{os}-{arch}`
4. airgate-core 插件市场会通过 GitHub API 自动同步新版本

这样 git tag = release 版本 = 已安装 tab 显示的版本，**单一来源、永不偏离**。

## License

MIT — 详见 [LICENSE](LICENSE)。
