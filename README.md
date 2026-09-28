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
- **fail-closed 的权限模型**：注册业务端点必须声明权限码（否则启动 panic），
  未登录一律 401，缺权限一律 403；公开端点集合由测试钉死

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

systemd unit 与 nginx 配置都在仓库里，可直接用：

```bash
sudo cp deploy/mini-ruoyi.service /etc/systemd/system/
```

unit 里已经设好 `GOMEMLIMIT=700MiB` / `GOGC=50` / `GOMAXPROCS=1`——
**不要删**，否则 GC 会一路吃到机器上限被 OOM Killer 干掉，症状是「进程莫名重启」。

完整的部署、备份、升级、排查见 [docs/deployment.md](docs/deployment.md)。

前端目录按以下顺序查找，取第一个含 `index.html` 的：

1. 二进制同级目录下的 `web/`（部署形态）
2. `./web`（工作目录就是仓库根）
3. `../bin/web`、`bin/web`（**用 IDE 或 `go run` 启动时的常见情况**）

也可以用 `APP_WEB_DIR` 显式指定。

> ⚠️ **用 GoLand / VS Code 直接运行 `cmd/server` 时**：二进制在临时目录、工作目录是 `server/`，
> 早期版本只找「二进制同级」会找不到前端，然后**静默降级成纯 API 模式**——
> 表现为「打开 :8080 看到一句 `{"code":1,"msg":"error.notFound"}`」。
> 现在会命中 `../bin/web`；若仍未命中，日志会明确告诉你执行 `make build` 或设置 `APP_WEB_DIR`。

**更新前端不需要重启后端**：重新执行 `make web`，把 `dist/` 内容同步到服务器的 `web/` 目录，用户刷新页面就能拿到新版本。
（`index.html` 响应头是 `no-cache`，`assets/*` 是 `immutable` + 内容哈希文件名，两者配合才能做到刷新即最新。）

## 部署形态

前端是纯静态文件，谁托管都行。后端在 `web/` 不存在时会**降级成纯 API 模式**，
因此「Go 直接托管」「Vite 本地调试」「nginx 托管前端」三种都能跑，
只是后两者需要额外配置。详见 [docs/architecture.md](docs/architecture.md) §5.1。

## 配置

**配置文件 + 环境变量**，优先级 **环境变量 > 配置文件 > 内置默认值**。
环境变量优先是为了让 systemd / 容器覆盖单个值，而不必去改文件。
**没有配置文件也能跑**——所有项都有默认值。

```bash
cp server/config/config.example.yaml server/config/config.yaml   # 按需修改，可选
```

`config.yaml` 在 `.gitignore` 里（含监听地址、代理地址这类部署相关信息）；
可提交的样例是 `server/config/config.example.yaml`。

```yaml
server:
  addr: ":8080"
  env: prod                          # dev 时用 gin 调试模式
  secure_cookie: false               # 前面有 TLS 终止层时必须 true
  trusted_proxies: []                # nginx 前置时必须填代理地址
  rate_limit: { rps: 20, burst: 40 } # 按 IP

database:
  path: data.db                      # 相对进程工作目录

web:
  dir: ""                            # 留空 = 自动查找前端产物目录

upload:
  dir: uploads
  max_mb: 20                         # 单文件上限
  quota_mb: 512                      # 总容量上限，不能小于 max_mb

log:
  retention_days: 30                 # 操作日志与登录日志保留天数
```

**配置错了会拒绝启动**并指出具体位置，不会静默用默认值：

```
加载配置: 解析配置文件 config/config.yaml: field max_size not found in type config.Upload
加载配置: 环境变量取值非法: APP_UPLOAD_MAX_MB="二十"
加载配置: upload.quota_mb（10）不能小于 upload.max_mb（100），否则一个文件都传不上去
```

启动时会打印一行生效配置摘要（路径一律是绝对路径，「数据在哪」「文件传到哪」一眼可见）：

```
监听 :8080（prod）| 数据库 /opt/mr/data.db | 前端 /opt/mr/web | 上传 /opt/mr/uploads
（单文件 20 MiB / 共 512 MiB）| 日志保留 30 天 | 可信代理 127.0.0.1
```

### 环境变量

全部可选，用于覆盖配置文件里的同名项。

| 变量 | 默认值 | 对应配置项 |
| --- | --- | --- |
| `APP_CONFIG` | `config/config.yaml` | 配置文件路径本身 |
| `APP_ADDR` | `:8080` | `server.addr` |
| `APP_ENV` | `prod` | `server.env`（`dev` 时用 gin 调试模式） |
| `APP_SECURE_COOKIE` | `false` | `server.secure_cookie` |
| `APP_TRUSTED_PROXIES` | 空（不信任任何代理） | `server.trusted_proxies`（逗号分隔） |
| `APP_RATE_LIMIT_RPS` | `20` | `server.rate_limit.rps` |
| `APP_RATE_LIMIT_BURST` | `40` | `server.rate_limit.burst` |
| `APP_DB_PATH` | `data.db` | `database.path` |
| `APP_WEB_DIR` | 自动查找 | `web.dir` |
| `APP_UPLOAD_DIR` | `uploads` | `upload.dir` |
| `APP_UPLOAD_MAX_MB` | `20` | `upload.max_mb` |
| `APP_UPLOAD_QUOTA_MB` | `512` | `upload.quota_mb` |
| `APP_LOG_RETENTION_DAYS` | `30` | `log.retention_days` |
| `GOMEMLIMIT` | 无 | Go 堆软上限，1G 机器建议 `700MiB` |

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
make check       提交前门禁：gofmt + go vet + 交叉编译 + 后端测试 + 前端类型检查
make test        后端测试（强制 -count=1，原因见 docs/architecture-server.md）
make test-e2e    浏览器端测试（会先构建，再用临时库起一个后端）
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
| [docs/schema.md](docs/schema.md) | **数据库 Schema**：表与字段定义、DDL、权限模型、字段取舍决策记录 |
| [docs/deployment.md](docs/deployment.md) | **部署与运维**：systemd、nginx、备份恢复、升级、排查 |
| [docs/architecture.md](docs/architecture.md) | 整体架构：进程模型、前后端契约、关键决策与权衡 |
| [docs/architecture-server.md](docs/architecture-server.md) | 后端架构：分层、中间件链、响应契约、数据层、迁移 |
| [docs/architecture-web.md](docs/architecture-web.md) | 前端架构：响应式、i18n、与后端的集成方式 |
| [server/README.md](server/README.md) | 后端项目说明：API 列表、测试、如何加迁移 |
| [web/README.md](web/README.md) | 前端项目说明：开发流程、如何加文案与组件 |

## 当前状态

**已实现**：分层骨架、统一响应契约、SQLite 连接与迁移、静态资源托管、按 IP 限流、
前端工具链与 i18n。

**后端已完成**：认证（session + CSRF）、RBAC 鉴权（33 个受权限保护的端点，权限码在代码里声明并启动校验）、
用户 / 角色 / 菜单 / 权限码的完整 CRUD、删除确认与守卫、版本化迁移。
表结构见 [docs/schema.md](docs/schema.md)，接口见 [server/README.md](server/README.md)。

含**系统监控**（在线会话可强制下线、登录日志、操作日志）与**系统工具**
（文件管理、定时任务）。日志保留 30 天可配，文件上传有单文件与总量双重限制。

**尚未实现**：

- 多标签页的**状态持久化**（刷新后只保留当前页的标签）
- 列表导出（CSV / Excel）
- 任务执行历史（现在 job 行上只存最近一次结果）
- 登录失败锁定

详见 [docs/architecture.md](docs/architecture.md) 的待办清单。

## 许可证

[MIT](LICENSE)

