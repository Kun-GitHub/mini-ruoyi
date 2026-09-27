# mini-ruoyi

跑在 1 核 1G 服务器上的极简管理系统。后端是一个 Go 单体进程（Gin + SQLite），前端是 Svelte 5 单页应用，
**由后端直接从磁盘托管前端构建产物**——因此部署时不需要 nginx，运行时不需要 Node，也不需要独立的数据库服务。

## 特性

- **单个二进制 + 一个前端目录**：`bin/mini-ruoyi` 和 `bin/web/`，`scp` 上去就能跑，一个 systemd unit 搞定
- **前端可独立更新**：前端产物不 embed 进二进制，重新构建后无需重启后端，浏览器刷新即生效
- **零 CGO**：SQLite 用 `modernc.org/sqlite`（纯 Go 实现），`GOOS/GOARCH` 交叉编译无障碍
- **无 ORM**：`database/sql` + 手写 SQL，SQL 可读可调
- **版本化迁移**：启动时按序号执行 `migrations/*.sql`，通过 `schema_migrations` 表跳过已执行项
- **语言中立的 API**：后端只输出稳定的 i18n 键，文案由前端按 `zh-CN` / `en-US` 渲染
- **前端 i18n**：手写约 110 行，零运行时依赖，字典缺键直接编译报错
- **面向低配机器**：按 IP 限流 20 rps（突发 40）、请求体上限 1 MiB、SQLite 连接池收敛到 4 条、WAL + `synchronous=NORMAL`

## 快速开始

需要 Go 1.25+ 和 Node.js 20+（Node 仅用于构建前端）。

```bash
git clone <repo-url> && cd mini-ruoyi
make build     # 构建前端到 web/dist，再编译二进制到 bin/，并把前端放到 bin/web/
make run       # 启动，默认监听 :8080
```

打开 <http://localhost:8080>。

只想跑后端 API：

```bash
make dev-server            # go run，web/ 目录不存在时自动降级为纯 API 模式
curl localhost:8080/healthz
```

前后端分离开发（前端热更新 + 后端 API）：

```bash
make dev-server            # 终端 1：后端 :8080
make dev-web               # 终端 2：Vite dev server :5173，/api 和 /healthz 代理到 :8080
```

## 部署

```
/opt/mini-ruoyi/
├── mini-ruoyi          # 二进制
├── web/                # 前端产物（index.html + assets/）
└── data.db             # SQLite 数据库，首次启动自动创建或使用随附的初始库
```

`systemd` unit：

```ini
[Unit]
Description=mini-ruoyi
After=network.target

[Service]
Type=simple
User=mini-ruoyi
WorkingDirectory=/opt/mini-ruoyi
ExecStart=/opt/mini-ruoyi/mini-ruoyi
Environment=APP_ADDR=:8080
Environment=APP_DB_PATH=/opt/mini-ruoyi/data.db
Restart=on-failure
# 1G 内存的机器上限制 Go 堆上限，避免被 OOM Killer 干掉
Environment=GOMEMLIMIT=700MiB

[Install]
WantedBy=multi-user.target
```

前端目录默认取「二进制同级目录下的 `web/`」，所以把二进制和 `web/` 放一起即可，不依赖 `WorkingDirectory`。

**更新前端不需要重启后端**：重新执行 `make web`，把 `dist/` 内容同步到服务器的 `web/` 目录，用户刷新页面就能拿到新版本。
（`index.html` 响应头是 `no-cache`，`assets/*` 是 `immutable` + 内容哈希文件名，两者配合才能做到刷新即最新。）

## 配置

全部通过环境变量，无配置文件。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `APP_ADDR` | `:8080` | HTTP 监听地址 |
| `APP_DB_PATH` | `data.db` | SQLite 文件路径，相对**进程工作目录** |
| `APP_WEB_DIR` | 二进制同级 `web/`，不存在时回落到 `./web` | 前端产物目录 |
| `APP_ENV` | `prod` | 设为 `dev` 时使用 `gin.DebugMode`，输出路由表与警告 |
| `GOMEMLIMIT` | 无 | Go 1.19+ 堆软上限，1G 机器建议 `700MiB` |

`APP_DB_PATH` 相对工作目录、`APP_WEB_DIR` 默认跟二进制走——两者解析规则不同是有意的：
数据库常放在数据盘或挂载卷，而前端产物天然跟二进制一起分发。

## 目录结构

```
mini-ruoyi/
├── Makefile                    # 构建入口
├── docs/                       # 架构文档
├── server/                     # Go 后端（go.mod 所在处，module 名 mini-ruoyi）
│   ├── cmd/server/main.go      # 启动、依赖组装、优雅关闭
│   ├── internal/               # 见 docs/architecture-server.md
│   └── data.db                 # 随仓库分发的初始数据库（开箱即用）
└── web/                        # Svelte 5 前端
    ├── src/                    # 见 docs/architecture-web.md
    └── dist/                   # 构建产物（不入库，由后端托管）
```

## 常用命令

```
make help        显示所有命令
make deps        安装前端依赖
make web         构建前端到 web/dist
make build       构建二进制 + 前端产物到 bin/
make run         构建后直接启动
make dev-server  启动后端（go run）
make dev-web     启动 Vite dev server
make clean       清理 bin/ 和 web/dist
```

## 文档

| 文档 | 内容 |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | 整体架构：进程模型、前后端契约、关键决策与权衡 |
| [docs/architecture-server.md](docs/architecture-server.md) | 后端架构：分层、中间件链、响应契约、数据层、迁移 |
| [docs/architecture-web.md](docs/architecture-web.md) | 前端架构：响应式、i18n、与后端的集成方式 |
| [server/README.md](server/README.md) | 后端项目说明：API 列表、测试、如何加迁移 |
| [web/README.md](web/README.md) | 前端项目说明：开发流程、如何加文案与组件 |

## 当前状态

**已实现**：分层骨架、统一响应契约、SQLite 连接与迁移、静态资源托管、按 IP 限流、
前端工具链与 i18n、`devices` 示例资源的完整 CRUD + 分页。

**尚未实现**：认证与鉴权（session / CSRF）、用户 / 角色 / 菜单 / API 权限（RBAC）表、
动态菜单与动态路由、前端业务页面。

`devices` 只是一个用于验证 CRUD、分页、错误契约和迁移链路的示例资源，不是业务功能。
详见 [docs/architecture.md](docs/architecture.md)。

## 许可证

Apache License 2.0。

> ⚠️ 仓库中多个文件头部声明"许可证见 LICENSE 文件"，但仓库当前**没有 LICENSE 文件**，
> 需要补上（见 [docs/architecture.md](docs/architecture.md) 的待办清单）。
