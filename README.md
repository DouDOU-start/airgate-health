<div align="center">
  <h1>AirGate Health</h1>

  <p><strong>AI 提供商账号健康监控插件</strong></p>

  <p>
    <a href="https://github.com/DouDOU-start/airgate-health/releases"><img src="https://img.shields.io/github/v/release/DouDOU-start/airgate-health?style=flat-square" alt="release" /></a>
    <a href="https://github.com/DouDOU-start/airgate-health/blob/master/LICENSE"><img src="https://img.shields.io/github/license/DouDOU-start/airgate-health?style=flat-square" alt="license" /></a>
    <a href="https://github.com/DouDOU-start/airgate-health/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/DouDOU-start/airgate-health/ci.yml?branch=master&style=flat-square&label=CI" alt="ci" /></a>
    <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go" alt="go" />
    <img src="https://img.shields.io/badge/React-19-61DAFB?style=flat-square&logo=react" alt="react" />
  </p>
</div>

---

AirGate Health 是 [airgate-core](https://github.com/DouDOU-start/airgate-core) 的健康监控扩展插件。它独立运行在自己的 gRPC 子进程里，定时主动探测每一个已配置的 AI 账号，把可用率与延迟沉淀到数据库，再以**管理员面板**和**对外公开状态页**两种形态暴露出来。

它解决一个具体问题：**网关对接十几家 AI 平台时，你不知道哪个 key 已经悄悄挂了**。等到用户报障再切号，已经晚了。Health 让账号健康度变成一份可量化、可订阅、可对外公示的数据。

## ✨ 核心特性

- **🔍 主动探测** — 周期性调用 SDK `TestAccount` 探测每个账号，结果落 `health_probes` 表，调度器带 worker pool 并发上限与 jitter，避免抖峰
- **📊 多维度聚合** — 按账号 / 分组 / 平台聚合可用率与 P50/P95 延迟，支持 7 / 15 / 30 / 90 天滑动窗口，预生成 90 天日桶用于趋势图
- **🎛 管理员面板** — 平台总览 + 分组聚合 + 单账号 90 天明细 + 「立即探测整组」一键触发，全部跑在 `/admin/health`
- **🌐 公开状态页** — 独立打包的 `status.html` 走 `/status` 路由对外公开，**严格脱敏**（不暴露 `account_id` / `error_msg`），仅展示分组维度可用率与方格图
- **⏱ 数据保留可控** — `retention_days` 控制原始探测记录清理周期；聚合查询天然支持长窗口，旧记录可放心丢
- **🔌 公开开关** — `public_status_enabled` 一键关闭公开页（直接 404），适合内部环境

## 🧩 接入位置

```text
                  ┌──────────────────────────────────────┐
                  │           AirGate Core               │
                  │   (账号、调度、计费、管理后台)        │
                  └────────────┬─────────────────────────┘
                               │ go-plugin (gRPC)
                               ▼
                  ┌──────────────────────────────────────┐
                  │       airgate-health (本仓库)        │
                  │                                      │
                  │   Prober (定时 / 手动)               │
                  │     │ TestAccount via CoreClient     │
                  │     ▼                                │
                  │   health_probes 表（PostgreSQL）     │
                  │     │                                │
                  │     ▼                                │
                  │   Aggregator (7/15/30/90d)           │
                  │     │                                │
                  │     ├──► /admin/health（管理员）     │
                  │     └──► /status（脱敏公开页）       │
                  └──────────────────────────────────────┘
```

Health 复用 core 的 PostgreSQL 实例：插件的 `health_probes` 表与 core 的 `accounts` / `groups` 表共享一个连接。`db_dsn` 由 core 自动注入，管理员不需要手填。

## 🚦 路由

插件路径已被 core 的 `ExtensionProxy` 剥掉前缀，下表展示**外部访问路径**。

### 管理员入口（`/api/v1/ext/airgate-health/*`，需要 admin 角色）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/admin/overview`              | 平台 + 账号 总览 |
| GET  | `/admin/accounts`              | 账号摘要列表（`?platform=&window=`）|
| GET  | `/admin/accounts/{id}`         | 单账号详情 + 90 天日桶 |
| GET  | `/admin/groups`                | 分组维度聚合 |
| POST | `/admin/probe/group/{id}`      | 手动触发整组探测 |

### 公开入口（`/status/*`，无需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/status`                       | 静态 HTML 状态页 |
| GET  | `/status/api/summary`           | 脱敏的分组维度聚合（无 `account_id` / `error_msg`） |
| GET  | `/status/assets/*`              | 状态页静态资源 |

公开入口受 `public_status_enabled` 开关控制；关闭后直接返回 404，不暴露"路径存在但被拒绝"的信号。

## 🔧 配置

`db_dsn` / `core_base_url` / `admin_api_key` 由 core 自动注入，**管理员无需填写**。前提是已经在「系统设置 → 安全与认证」生成过 admin API key，然后在「插件管理」热加载本插件。

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `probe_interval_seconds` | int  | 60   | 周期性探测的最小间隔；过小会浪费上游配额 |
| `probe_concurrency`      | int  | 8    | worker pool 上限，限制同时进行的 `TestAccount` 数量 |
| `probe_timeout_seconds`  | int  | 15   | 单次 `TestAccount` 调用的硬超时 |
| `retention_days`         | int  | 30   | `health_probes` 表的清理周期；聚合查询足以覆盖 7/15/30 天窗口 |
| `public_status_enabled`  | bool | true | 开启后 `/status` 路由对外公开 |

## 📁 目录结构

```text
airgate-health/
├── backend/                              # Go 后端（插件主体）
│   ├── main.go                           # gRPC 插件入口
│   ├── cmd/genmanifest/                  # plugin.yaml 生成器
│   └── internal/health/
│       ├── metadata.go                   # PluginInfo + ConfigSchema + FrontendPages（运行时单源）
│       ├── plugin.go                     # ExtensionPlugin 实现：生命周期 + 路由 + 后台任务
│       ├── routes.go                     # admin / public 路由注册与中间件
│       ├── prober.go                     # 周期性 + 手动整组探测调度
│       ├── aggregator.go                 # 多窗口可用率 / 延迟 / 日桶聚合查询
│       ├── core_client.go                # 回调 core 拉账号 / 触发 TestAccount
│       ├── db.go                         # health_probes 表 schema + 清理任务
│       ├── assets.go                     # WebAssetsProvider，embed webdist
│       └── webdist/                      # build 时由 web/dist 同步过来（go:embed）
├── web/                                  # 前端
│   ├── src/                              # 管理员面板（/admin/health）
│   ├── status.html                       # 独立打包的公开状态页
│   ├── vite.config.ts                    # admin 面板构建配置
│   └── vite.status.config.ts             # status.html 构建配置
├── .github/workflows/
│   ├── ci.yml                            # push/PR 触发，复用 make ci
│   └── release.yml                       # v* tag 触发，矩阵构建 4 平台二进制
├── plugin.yaml                           # genmanifest 自动生成
└── Makefile
```

## 🚀 构建与开发

### 安装到 core

打开 core 管理后台 → **插件管理** → 三种方式任选：

```text
1. 插件市场 → 点击「安装」    （从 GitHub Release 自动拉取，匹配当前架构）
2. 上传安装 → 拖入二进制文件   （适合内部环境）
3. GitHub 安装 → 输入 DouDOU-start/airgate-health
```

### 本地开发

需要 Go 1.25+、Node 22+、本地 PostgreSQL，以及兄弟目录 [`airgate-sdk`](https://github.com/DouDOU-start/airgate-sdk) 与 [`airgate-core`](https://github.com/DouDOU-start/airgate-core)：

```bash
make install        # 装 web 依赖与 Go 模块
make build          # 完整构建：web/dist → backend/webdist → bin/airgate-health
make manifest       # 重新生成 plugin.yaml
make ci             # 与 CI 完全一致的本地检查（type-check + vet + test + build）
```

把本插件以 dev 模式挂到 core，热重载不重启 core：

```yaml
# airgate-core/backend/config.yaml
plugins:
  dev:
    - name: airgate-health
      path: /absolute/path/to/airgate-health/backend
```

然后 `cd airgate-core/backend && go run ./cmd/server`，core 会通过 `go run .` 启动本插件，握手 gRPC，依次调 `Init → Migrate → Start → RegisterRoutes`。

## 📦 发版

`metadata.go` 中的 `PluginVersion` 是 `var`，默认值仅用于本地开发。**正式发版只需要打 git tag，不要手工改版本号字段**：

```bash
git tag v0.2.0
git push origin v0.2.0
```

[release.yml](.github/workflows/release.yml) 工作流会自动：

1. 矩阵构建 4 个平台二进制（linux/darwin × amd64/arm64）
2. 通过 `-ldflags "-X .../health.PluginVersion=${version}"` 把 git tag（去掉 `v` 前缀）注入到二进制
3. 上传到 GitHub Release，资产命名 `airgate-health-{os}-{arch}`，附带 `.sha256`
4. airgate-core 插件市场会通过 GitHub API 自动同步新版本

git tag = release 版本 = 已安装 tab 显示的版本，**单一来源、永不偏离**。

## 🤝 反馈

- Bug / Feature: [Issues](https://github.com/DouDOU-start/airgate-health/issues)
- 主仓库: [airgate-core](https://github.com/DouDOU-start/airgate-core)
- 插件 SDK: [airgate-sdk](https://github.com/DouDOU-start/airgate-sdk)

## 📜 License

MIT — 详见 [LICENSE](LICENSE)。
