# AirGate 健康监控插件 Makefile
#
# 典型工作流：
#   make install          # 安装前端 pnpm 依赖 + Go 模块
#   make dev              # 提示如何在 core 里 dev 加载本插件
#   make build            # 完整构建：web/dist → backend/webdist → bin/airgate-health
#   make manifest         # 重新生成 plugin.yaml

GO := GOTOOLCHAIN=local go

WEBDIST := backend/internal/health/webdist

.PHONY: help install build build-web build-backend release manifest dev ensure-webdist clean test vet lint fmt ci type-check pre-commit setup-hooks

help: ## 显示帮助信息
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ===================== 构建 =====================

install: ## 安装 web 依赖与 Go 模块
	cd web && pnpm install
	cd backend && $(GO) mod download
	@echo "依赖安装完成"

build: build-web build-backend ## 完整构建：前端 → 嵌入后端 → 编译

build-web: ## 构建插件前端
	cd web && pnpm build

build-backend: ensure-webdist ## 构建后端二进制
	cd backend && $(GO) build -o ../bin/airgate-health .

release: build-web ## 编译 Linux amd64 版本（用于上传到 marketplace）
	rm -rf $(WEBDIST)
	cp -r web/dist $(WEBDIST)
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -o ../bin/airgate-health-linux-amd64 .
	@echo "构建完成: bin/airgate-health-linux-amd64"

# 把 web/dist 同步到 backend/internal/health/webdist；若 web 未构建则保留 placeholder
ensure-webdist:
	@if [ -d web/dist ] && [ "$$(ls -A web/dist 2>/dev/null)" ]; then \
		rm -rf $(WEBDIST); \
		cp -r web/dist $(WEBDIST); \
		echo "已同步 web/dist → $(WEBDIST)"; \
	elif [ ! "$$(ls -A $(WEBDIST) 2>/dev/null)" ]; then \
		mkdir -p $(WEBDIST); \
		echo "placeholder" > $(WEBDIST)/placeholder.txt; \
		echo "未发现 web/dist，写入 placeholder（前端将不可用）"; \
	fi

# ===================== 开发 =====================

dev: build-web ## 构建前端资产并提示如何在 core 里 dev 加载本插件
	@echo "在 airgate-core/backend/config.yaml 的 plugins.dev 节追加："
	@echo ""
	@echo "  plugins:"
	@echo "    dev:"
	@echo "      - name: airgate-health"
	@echo "        path: $(realpath ./backend)"
	@echo ""
	@echo "然后启动 core: cd airgate-core/backend && go run ./cmd/server"

manifest: ## 重新生成 plugin.yaml
	cd backend && $(GO) run ./cmd/genmanifest

# ===================== 质量检查 =====================

ci: ensure-webdist lint vet test build-backend ## 本地运行与 CI 完全一致的检查

pre-commit: ensure-webdist lint vet test ## pre-commit hook 调用（test 会跑 cmd/genmanifest 拦截 plugin.yaml 漂移）

lint: ## 代码检查（需要安装 golangci-lint）
	@if ! command -v golangci-lint > /dev/null 2>&1; then \
		echo "错误: 未安装 golangci-lint，请执行: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
		exit 1; \
	fi
	@cd backend && golangci-lint run ./...
	@cd web && pnpm exec tsc --noEmit
	@cd web && pnpm lint
	@echo "代码检查通过"

fmt: ## 格式化代码
	@cd backend && \
	if command -v goimports > /dev/null 2>&1; then \
		goimports -w -local github.com/DouDOU-start .; \
	else \
		$(GO) fmt ./...; \
	fi
	@echo "代码格式化完成"

type-check: ## 前端 TypeScript 类型检查
	cd web && pnpm type-check

test: ensure-webdist ## 运行后端测试
	cd backend && $(GO) test ./...

vet: ensure-webdist ## 静态分析
	cd backend && $(GO) vet ./...

# ===================== Git Hooks =====================

setup-hooks: ## 安装 Git hooks（pre-commit + commit-msg）
	@echo '#!/bin/sh' > .git/hooks/pre-commit
	@echo 'make pre-commit' >> .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@cp scripts/commit-msg .git/hooks/commit-msg
	@chmod +x .git/hooks/commit-msg
	@echo "Git hooks 已安装（pre-commit + commit-msg）"

# ===================== 清理 =====================

clean: ## 清理构建产物
	rm -rf bin/ web/dist
	rm -rf $(WEBDIST)
	mkdir -p $(WEBDIST)
	echo "placeholder" > $(WEBDIST)/placeholder.txt
