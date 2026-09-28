.PHONY: help deps web build clean run test check test-e2e dev-server dev-web db-clean

# Windows 上可执行文件必须带 .exe：不带扩展名的文件 CreateProcess 直接报 ENOENT，
# build 出来的东西跑不起来，E2E 的 spawn 也找不到它。
ifeq ($(OS),Windows_NT)
EXE := .exe
endif

BIN := bin/mini-ruoyi$(EXE)

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
	cd bin && ./mini-ruoyi$(EXE)

# -count=1 不是可选项：httpx / perm 里有几个用例会读取前端 i18n 字典，
# 而 Go 的测试缓存不追踪测试运行期 os.ReadFile 打开的文件——不加这个标志，
# 改了前端字典后 go test 会直接返回缓存里那个已经过期的 "ok"。
test: ## 跑全部后端测试（-count=1，绕过对前端字典的过期缓存）
	cd server && go test -count=1 ./...

check: ## 提交前门禁：格式 + vet + 交叉编译 + 后端测试 + 前端类型检查
	@cd server && files=$$(gofmt -l .); \
		if [ -n "$$files" ]; then echo "以下文件未 gofmt: $$files"; exit 1; fi
	cd server && go vet ./...
	$(MAKE) check-cross
	$(MAKE) test
	cd web && npm run check

# data.db 既随仓库分发（clone 即可用），也是开发时的活数据库 —— 从 server/ 启动一次
# 再登录，会话与日志就写进去了，提交时会一起带上。这条把它还原成「刚跑完迁移」的状态。
# 需要 sqlite3 命令行工具（仅开发维护用，**运行本项目本身不需要**）。
db-clean: ## 清掉 data.db 里开发期产生的数据（提交前跑一次）
	@command -v sqlite3 >/dev/null 2>&1 || { \
		echo "需要 sqlite3 命令行工具（macOS: brew install sqlite　Debian: apt install sqlite3）"; exit 1; }
	@cd server && sqlite3 data.db " \
		DELETE FROM sys_user_roles WHERE user_id IN (SELECT id FROM sys_users WHERE username <> 'admin'); \
		DELETE FROM sys_role_menus WHERE role_id   IN (SELECT id FROM sys_roles WHERE code    <> 'admin'); \
		DELETE FROM sys_role_perms WHERE role_id   IN (SELECT id FROM sys_roles WHERE code    <> 'admin'); \
		DELETE FROM sys_users WHERE username <> 'admin'; \
		DELETE FROM sys_roles WHERE code <> 'admin'; \
		DELETE FROM sys_sessions; DELETE FROM sys_login_logs; DELETE FROM sys_oper_logs; DELETE FROM sys_files; \
		UPDATE sys_users SET login_ip = '', login_at = NULL; \
		UPDATE sys_jobs  SET last_run_at = NULL, last_status = '', last_error = '', last_duration_ms = 0; \
		DELETE FROM sqlite_sequence;" \
	&& sqlite3 data.db "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null \
	&& echo "data.db 已清理：会话 / 登录日志 / 操作日志 / 文件记录 / 非 admin 账号 / 任务执行痕迹" \
	&& echo "（磁盘上的 uploads/ 是 gitignore 的，不随仓库分发，可另行手动清空）"

# 目标平台是 Linux，但开发常在 macOS/Windows —— 平台相关的代码
# （比如 syscall.Statfs 只有 Unix 有）在本机编译得过并不代表在别处也能过。
# 这条专门防「用户在自己机器上一编译就报 undefined」。
check-cross:
	@cd server && for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
		GOOS=$${target%/*} GOARCH=$${target#*/} go build ./... || exit 1; \
	done && echo "交叉编译通过（linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64）"

test-e2e: build ## 浏览器端测试（会先构建，再用临时库起一个后端）
	cd web && npx playwright test

dev-server: ## 启动后端（仅 API，前端走 Vite dev server）
	go run -C server ./cmd/server

dev-web: ## 启动 Vite dev server（/api 与 /healthz 代理到 :8080）
	cd web && npm run dev

clean: ## 清理构建产物
	rm -rf bin web/dist
