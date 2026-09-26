.PHONY: help deps web build clean run dev-server dev-web

BIN := bin/mini-ruoyi

help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

deps: ## 安装前端依赖
	cd web && npm install --no-audit --no-fund

web: ## 构建前端到 web/dist
	cd web && npm run build

# 二进制与 web/ 同级，后端据此自动定位前端目录（见 internal/config.defaultWebDir）
build: web ## 构建二进制到 bin/，并把前端产物放到 bin/web/
	rm -rf bin
	mkdir -p bin
	cp -R web/dist bin/web
	go build -C server -o ../$(BIN) ./cmd/server

run: build ## 构建后直接启动
	cd bin && ./mini-ruoyi

dev-server: ## 启动后端（仅 API，前端走 Vite dev server）
	go run -C server ./cmd/server

dev-web: ## 启动 Vite dev server（/api 与 /healthz 代理到 :8080）
	cd web && npm run dev

clean: ## 清理构建产物
	rm -rf bin web/dist
