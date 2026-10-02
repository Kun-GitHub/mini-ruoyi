# 后端架构

[English](architecture-server.en.md) | 简体中文

Go 1.25 + Gin 1.10 + `database/sql` + SQLite（`modernc.org/sqlite`，纯 Go 无 CGO）。零 ORM、零额外服务。

## 1. 目录结构

```
server/
├── go.mod                          # module mini-ruoyi
├── data.db                         # 随仓库分发的初始数据库
├── config/config.example.yaml      # 带注释的配置样例（config.yaml 本身被 gitignore）
├── cmd/server/main.go              # 启动、依赖组装、优雅关闭
└── internal/
    ├── config/config.go            # 配置结构与加载（文件 + 环境变量）
    ├── domain/                     # 实体 + 领域错误（最内层，不依赖任何包）
    │   ├── consts.go               # 状态、菜单类型、内置角色编码
    │   ├── errors.go               # 错误哨兵 + DependentsError / DuplicateError
    │   ├── user.go · rbac.go       # User；Role / Menu / MenuNode
    │   ├── session.go · log.go     # Session / SessionView；LoginLog / OperLog / JobLog
    │   └── file.go · job.go        # File / FileUsage；Job
    ├── httpx/response.go           # 统一响应信封、错误键、错误→HTTP 状态码映射
    ├── perm/perm.go                # 权限码常量与分组，不查库
    ├── auth/                       # 密码哈希、会话签发与解析
    ├── repository/
    │   ├── sqlite.go               # 连接与连接池
    │   ├── migrate.go              # 迁移执行器
    │   ├── time.go · filter.go     # 时间存储格式；LIKE 转义与 where 条件拼装
    │   ├── migrations/*.sql        # 版本化 DDL + 种子数据
    │   └── *_repository.go         # 单表 CRUD：user / role / menu / session / log / file / job
    ├── service/                    # 业务规则、事务边界、分页归一化
    │   ├── paging.go               # Page[T] 与页码归一化
    │   ├── authz_service.go        # 权限判定（管理员铺满全部权限码）
    │   ├── log_service.go          # 审计日志缓冲与批量落库
    │   └── *_service.go            # user / role / menu / file / job / monitor
    ├── system/                     # 采集 CPU/内存/磁盘/进程（只读 OS 探测，不依赖库）
    │   ├── system.go               # Snapshot 组装与 /proc 解析
    │   └── disk_unix.go · disk_windows.go   # Statfs vs GetDiskFreeSpaceExW
    ├── job/                        # job.go 注册表；defs.go 任务定义
    ├── handler/                    # HTTP 适配：绑定、校验、响应
    ├── middleware/                 # auth.go 会话与 CSRF；operlog.go 审计；recovery.go 信封化 panic
    └── httpserver/
        ├── router.go               # 路由注册、中间件装配
        ├── registrar.go            # 路由登记入口（公开 / 自助 / 受保护三类）
        └── static.go               # 磁盘静态托管 + SPA 兜底
```

## 2. 分层与依赖方向

依赖严格单向，无环：

```
domain      →（不依赖任何内部包）
config      →（不依赖任何内部包）
system      →（不依赖任何内部包）
job         →（不依赖任何内部包）
httpx       → domain
perm        →（不依赖任何内部包）
auth        → domain, repository
repository  → domain
service     → auth, domain, job, perm, repository, system
handler     → auth, domain, httpx, middleware, perm, repository, service
middleware  → auth, domain, httpx, perm, service
httpserver  → handler, httpx, middleware, perm
```

| 层 | 职责 | 禁止 |
| --- | --- | --- |
| `domain` | 实体定义、领域错误哨兵（`ErrNotFound` / `ErrHasDependents` / `ErrDuplicate` 等） | 依赖任何其他内部包 |
| `config` | 配置装配（环境变量 > 文件 > 默认值），只被 `cmd/server` 使用 | 依赖任何其他内部包 |
| `system` | 采集 CPU / 内存 / 磁盘 / 进程；逐项带 `available` 标记，读不到就说读不到 | 依赖 service / handler |
| `job` | 定时任务注册表（任务真源在代码，库里只存开关与 cron）；依赖用接口声明在使用方 | 依赖 service / auth（会成环） |
| `perm` | 权限码常量与分组，不查库 | 依赖任何其他内部包 |
| `auth` | 密码哈希/校验、会话签发与解析 | 依赖 handler / service |
| `httpx` | 响应信封、错误键、错误→状态码映射 | 依赖 handler / service / repository |
| `repository` | 单表 SQL，行 ↔ 实体 | 包含业务规则、跨表事务编排 |
| `service` | 业务规则、事务边界、分页归一化 | 依赖 `*gin.Context`、构造 HTTP 响应 |
| `handler` | 参数绑定、调用 service、写响应 | 写 SQL、包含业务规则（判错只用 `domain` 的错误哨兵） |
| `middleware` | 横切关注点 | 依赖 handler |
| `httpserver` | 路由与中间件装配、静态资源 | 包含业务逻辑 |

两条硬规则：

1. **判错统一用 `domain` 的错误哨兵**（`ErrNotFound` / `ErrHasDependents` / `ErrDuplicate` …），
   状态码映射集中在 `httpx.FailFromError` 一处，handler 里不该出现任何 `switch` 状态码。
   handler 会 import `repository`，但只用来组装列表筛选条件（`repository.Filter` 家族），
   不写 SQL、不做业务判断——历史上这里是真正的跨层泄漏（handler 自己写查询），已修正。
2. **响应信封住在 `httpx` 而非 `handler`。** middleware 和 httpserver 同样要写响应
   （401/403/404/429/503），若信封住在 handler 中，这些包就得反向依赖 handler。

## 3. 启动流程

`cmd/server/main.go`：

```
config.Load()                        读环境变量
  ↓
gin.SetMode(Debug/Release)          由 APP_ENV 决定
  ↓
repository.NewDB(cfg.DBPath)         打开连接 + Ping（Ping 失败即退出，配置错误尽早暴露）
  ↓
repository.Migrate(ctx, db)          按序执行未应用的迁移
  ↓
手工组装 repo → service → handler     不引入 wire/fx
  ↓
httpserver.NewRouter(...)            路由 + 中间件；前端目录结构不完整则返回错误
  ↓
ListenAndServe + 等待 SIGINT/SIGTERM
  ↓
srv.Shutdown(15s)                    优雅关闭
```

超时配置（`main.go` 常量）：

| 项 | 值 | 说明 |
| --- | --- | --- |
| `readTimeout` | 5s | 读整个请求（含 body）的期限，同时兜住 header 读取，防 Slowloris |
| `writeTimeout` | 10s | 写响应期限 |
| `idleTimeout` | 60s | keep-alive 空闲期限 |
| `shutdownTimeout` | 15s | **必须大于 `writeTimeout`**，否则会掐断进行中的响应 |

## 4. 中间件链

装配顺序即执行顺序（`httpserver/router.go`）：

```
1. middleware.Recovery()                             panic → 500（仍是信封，不是空 body）
2. middleware.RequestLogger()                       方法 路径 状态码 耗时
3. SetTrustedProxies(APP_TRUSTED_PROXIES)           ← 不是中间件，是路由配置，但要早于限流
4. middleware.BodyLimit(1 MiB)                      请求体上限（排除 /api/v1/files）
5. middleware.RateLimit(20 rps, burst 40, TTL 3m)   按 IP 限流
6. middleware.ImmutableAssets("/assets/")           静态资源长缓存头
   ── 路由 ──
   GET  /healthz                                     → SQLite Ping
   /assets/*filepath
   /api/v1   7→8→9 对整组生效，10 只挂在 protect 注册的路由上
   │   7. Auth.Require()                             无会话 → 401
   │   8. Auth.CSRF()                                写请求缺令牌 → 403
   │   9. middleware.OperationLog(...)                写操作与 401/403 记审计
   │  10. middleware.RequirePerm(code)                缺权限码 → 403
   └── 菜单 / 角色 / 用户 / 权限清单 / 文件 / 任务 / 监控 / 会话 / 日志
   ── NoRoute ──
   /api/* 与 /assets/* → JSON 404；其余 → index.html（SPA 兜底）
```

三条顺序约束，动装配顺序前先看这里：

| 约束 | 为什么 |
| --- | --- |
| `SetTrustedProxies` 早于 `RateLimit` | 限流按 `c.ClientIP()` 计数，代理配置比它晚就等于没配 |
| `Require` 早于 `CSRF` | 先确认「是谁」，令牌才有归属可比 |
| `OperationLog` 早于 `RequirePerm` | 越权试探（403）也要被记录下来 |

`RequirePerm` 挂在每条受保护路由上而不是 `r.Use`：权限码本来就是逐路由的，
装配时自动带上就不可能漏。`httpserver/registrar.go` 只有三个登记入口——
`open`（必须写明「为什么不登录」，理由会被测试断言）、`self`（仅登录，操作自己的数据）、
`protect`（登录 + 权限码；码必须是 `internal/perm` 声明过的，写错启动即 panic）。

### 4.1 路由清单

当前 39 个端点：**1 公开 + 4 自助 + 34 受权限保护**（下表省略 `/api/v1` 前缀）。

| 分组 | 端点 |
| --- | --- |
| 公开 | `POST /auth/login` |
| 自助（`self`） | `GET /auth/me`、`POST /auth/logout`、`PUT /profile`、`PUT /profile/password` |
| 菜单 | `GET /menus`、`GET /menus/:id`、`POST /menus`、`PUT /menus/:id`、`DELETE /menus/:id` |
| 角色 | `GET /roles`、`GET /roles/:id`、`GET /roles/:id/grants`、`POST /roles`、`PUT /roles/:id`、`PUT /roles/:id/grants`、`DELETE /roles/:id` |
| 用户 | `GET /users`、`GET /users/:id`、`POST /users`、`PUT /users/:id`、`PUT /users/:id/roles`、`PUT /users/:id/password`、`DELETE /users/:id` |
| 权限清单 | `GET /perms` |
| 文件 | `GET /files`、`GET /files/:id/download`、`POST /files`、`DELETE /files/:id` |
| 任务 | `GET /jobs`、`PUT /jobs/:key`、`POST /jobs/:key/run`、`GET /jobs/:key/logs` |
| 监控 | `GET /system`、`GET /sessions`、`DELETE /sessions/:hash`、`DELETE /users/:id/sessions`、`GET /login-logs`、`GET /oper-logs` |

读与写的权限码分开声明：「能看」等于「能改」是常见的授权漏洞，所以
`GET /roles/:id/grants` 用 `list` 码而 `PUT /roles/:id/grants` 用 `edit` 码，
`POST /jobs/:key/run`（立刻跑一次）与 `PUT /jobs/:key`（改调度）也各用各的。

反过来，**读接口不另开权限码**：执行历史挂在现有任务的 `list` 码下面。
「这个任务上次跑成什么样」本来就在列表页上，再要一个码意味着
既有角色升级后突然看不到自己原本能看到的东西。

### 4.2 两个容易踩的坑

**限流必须按 IP，不能全进程。** 全进程令牌桶意味着单个客户端刷满配额后，其他所有用户
一起收到 429——限流器反而成了 DoS 放大器。同时限流器 map 必须惰性 GC，
否则被扫描时 map 会随访问过的 IP 无上限增长。

**不能信任 `X-Forwarded-For`。** 本服务直接对外监听，没有 nginx。若不调用
`SetTrustedProxies(nil)`，gin 的 `ClientIP()` 会采信客户端自带的 `X-Forwarded-For`，
限流可以被一行 header 绕过。测试用例 `TestRateLimitIgnoresSpoofedForwardedFor` 覆盖这一点。

## 5. 响应信封（`internal/httpx`）

```go
type Response struct {
    Code   int          `json:"code"`             // 恒等于 HTTP 状态码
    Msg    string       `json:"msg"`              // 成功 "ok"；失败为 i18n 键
    Data   any          `json:"data,omitempty"`
    Errors []FieldError `json:"errors,omitempty"`
}

type FieldError struct {
    Field string `json:"field"`           // JSON 字段名
    Rule  string `json:"rule"`            // validator tag：required / min / max / ...
    Param string `json:"param,omitempty"` // 规则参数
}
```

**后端不产出面向用户的文案**，只产出稳定的 i18n 键（`error.notFound` 等）。文案由前端渲染。
这是为了支持多语言：后端一旦写死中文，前端就没法翻译。

### 5.1 两个错误映射入口

分两个函数是因为它们的语义不同：

| 函数 | 处理 | 映射 |
| --- | --- | --- |
| `FailBindError` | 请求体解析/校验失败 | `*http.MaxBytesError` → 413；`validator.ValidationErrors` → 400 + `errors[]`；其余 → 400 `error.malformedBody` |
| `FailFromError` | 领域/业务错误 | `domain.ErrNotFound` → 404；其余 → 500 `error.internal` + **落日志** |

`FailFromError` 的 5xx 分支必须落日志：客户端只会拿到一个 `error.internal` 键，
排查只能靠服务端日志。

### 5.2 字段名用 JSON tag

`RegisterJSONFieldNames()` 在 `NewRouter` 里调用，给 gin 的全局校验器注册 tag name func，
使校验错误里的字段名是 `name` 而不是 `Name`。必须在注册路由前调用，只需一次。

## 6. 数据层

### 6.1 连接配置（`repository/sqlite.go`）

```go
var sqlitePragmas = func() string {
    v := url.Values{}
    v.Add("_pragma", "busy_timeout(5000)")
    v.Add("_pragma", "journal_mode(WAL)")
    v.Add("_pragma", "foreign_keys(1)")
    v.Add("_pragma", "synchronous(NORMAL)")
    return v.Encode()
}()
```

### ⚠️ 为什么 PRAGMA 必须走 DSN

`busy_timeout` / `foreign_keys` / `synchronous` 都是**连接级**设置。用 `db.Exec("PRAGMA ...")`
执行只会命中连接池里的**某一条**连接，其余连接仍是默认值。实测（连接池上限 4）：

```
conn#0 foreign_keys=1 busy_timeout=5000 synchronous=1   ← 只有它被配置了
conn#1 foreign_keys=0 busy_timeout=0    synchronous=2
conn#2 foreign_keys=0 busy_timeout=0    synchronous=2
conn#3 foreign_keys=0 busy_timeout=0    synchronous=2
```

后果是 3/4 的请求遇写锁立刻 `SQLITE_BUSY`，且外键约束形同虚设。

`journal_mode=WAL` 是唯一例外——它写入数据库文件头，是持久设置，所以侥幸不出问题。
但它也一并放在 DSN 里，让行为集中。

**回归用例**：`repository/sqlite_test.go` 的 `TestNewDBConfiguresPragmasOnEveryConn`
同时占住 4 条连接逐条断言。**改动连接配置后必须让这个用例通过。**

连接池上限 4：SQLite 是单写者模型，写操作本身串行，多出的连接只服务并发读。

### 6.2 迁移（`repository/migrate.go`）

- 文件命名 `<版本号>_<描述>.sql`，放在 `internal/repository/migrations/`
- 通过 `//go:embed migrations/*.sql` 打包进二进制（迁移必须与代码版本同源，不能靠运维拷贝）
- 按文件名排序执行（零填充数字前缀，字典序即版本序）
- 每个文件在**单个事务**内执行迁移体 + 写入 `schema_migrations` 记录，两者要么都生效要么都不生效
  （SQLite 的 DDL 是事务性的，所以 DDL 失败不会留下半截结构）
- **只向前补齐，不支持回滚。** SQLite 的 DDL 回滚脚本容易和线上数据现状不一致。
  改错了就再加一个修复迁移

```sql
CREATE TABLE schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### 6.3 时间字段

DATETIME 能**直接扫进 `time.Time`**（驱动已做转换，无需 `_time_format` 参数）。
所以实体里的时间字段声明为 `time.Time`，JSON 序列化自动输出 RFC 3339 UTC。

不要用 `string`：SQLite 存的是 `2025-09-26 22:04:00` 这种格式，Safari 的 `new Date()` 解析不了。

### 6.4 空列表

`List` 用 `make([]T, 0, limit)` 而不是 `var list []T`，确保空结果序列化成 `[]` 而非 `null`。
否则前端拿到 `data.list === null` 会崩。菜单树这类不分页的查询用 `make([]Menu, 0)`——
关键是最初那个 `0` 长度，不是容量。

## 7. 静态资源托管（`httpserver/static.go`）

前端产物从磁盘读取，**不 embed 进二进制**，因此重新构建前端后无需重启后端。

### 7.1 两种部署形态

| `APP_WEB_DIR` 状态 | 行为 |
| --- | --- |
| 整个目录不存在 | 打印警告，降级为**纯 API 模式**，所有非 API 路径返回 JSON 404 |
| 目录存在但缺 `index.html` 或 `assets/` | **启动即失败**并给出明确提示 |
| 目录完整 | 挂载 `/assets`，其余路径回落 `index.html` |

区分前两者是为了同时照顾"我只要 API"和"我前端部署错了"两种情形——后者必须尽早报错。

### 7.2 路由与缓存

```
/assets/*   http.FileServer(http.Dir(webDir/assets))  + Cache-Control: immutable
其余路径     index.html                               + Cache-Control: no-cache
/api/*      不回落，返回 JSON 404
```

`/api/*` 和 `/assets/*` **绝不能**回落到 `index.html`，否则前端会把一整页 HTML 当 JSON
解析，报错信息完全看不出真正原因。测试用例 `TestAPINotFoundReturnsJSON` 与
`TestMissingAssetIsNotHtml` 覆盖这一点。

> 注：`/assets/` 的 404 是由 gin 的 `createStaticHandler` 在 `fs.Open` 失败时把
> `c.handlers` 换成 `noRoute` 触发的，所以确实会走到 NoRoute 分支，不是死代码。

## 8. 配置（`internal/config`）

加载顺序：**内置默认值 → 配置文件 → 环境变量**，然后校验。
详见 [architecture.md](architecture.md) §5.5 里的取舍说明。

配置文件默认在 `config/config.yaml`（可用 `APP_CONFIG` 改），
**不存在也不算错**——那样才能 clone 下来直接跑。
解析用 `KnownFields(true)`，写错字段名会直接报错而不是静默忽略。

**完整的环境变量与配置项清单在 [../server/README.md](../server/README.md)**——
不在这里再抄一份，两份表必然漂移。

两处路径解析规则值得单独说明，它们**故意不同**：

| 配置项 | 解析规则 | 为什么 |
| --- | --- | --- |
| `database.path` | 相对**进程工作目录** | 数据库常放在数据盘或挂载卷上，位置由部署决定，不该跟着二进制走 |
| `web.dir`（留空时自动查找） | 二进制同级 `web/` → `./web` → `../bin/web` → `bin/web` | 前端产物天然跟二进制一起分发。多候选是为了覆盖 `go run` 与 IDE 启动——那时二进制在临时目录里，同级根本没有 `web/` |

前端目录找不到时会降级成纯 API 模式，并打印一句指明怎么修的提示。
早期版本只找「二进制同级」那一个位置，找不到就静默降级——表现为
「打开 :8080 看到一句 JSON 404」，很难联想到是前端目录的问题。

## 9. 测试

```bash
cd server && go test ./...        # 全部
go test ./... -run TestNewDB      # 单个
```

当前 130 个用例，分布在 18 个测试文件里。`handler` / `job` / `auth` / `domain` / `cmd/server`
没有独立测试文件——它们的正确性由 `httpserver` 的端到端用例覆盖（handler 的每个分支
基本都对应一条 HTTP 断言）。

| 文件 | 覆盖 |
| --- | --- |
| `repository/sqlite_test.go` | **每条连接的 PRAGMA 都生效**（防 A1 回归）、WAL 已启用 |
| `repository/migrate_test.go` | 迁移幂等；在旧版遗留库（有表无迁移记录）上升级不丢数据；**种子密码哈希能通过 bcrypt 校验**；**结构校验能发现「记录说已应用、表却不在」** |
| `repository/menu_seed_test.go` | **菜单种子与前端对得上**：每条菜单的 `component` 有对应页面、`title_key` 在双语字典里都存在、`path` 不重复；**反向也查**——页面文件不能有谁都点不进去的 |
| `repository/filter_test.go` | LIKE 通配符转义（`%` / `_` / `\`）、用户输入不被当成通配符、筛选真的命中预期行、**按角色筛不会因多角色把行数翻倍**、日志时间区间（`end` 含当天） |
| `service/rbac_test.go`（含 `authz_service` 的身份解析） | 删除影响面（含后代菜单的授权）、级联删除、环引用拦截、四条删除守卫、管理员身份解析、分页边界 |
| `perm/perm_test.go` | 分组覆盖与去重、键名推导约定、未知权限码检出、前端字典覆盖 |
| `repository/session_repository_test.go` | **datetime 存储格式**、时间往返、过期清理、按用户踢人、删用户级联 |
| `httpx/response_test.go` | 错误键唯一且命名规范、前端字典覆盖（含 **client.ts 里写死的那两个前端自产键**）、**后端全部字段名与校验规则都有对应文案键** |
| `config/config_test.go` | 没有配置文件用默认值、文件部分覆盖、环境变量覆盖文件、未知字段被拒、非法取值被拒、`web.dir` 的候选路径顺序 |
| `httpserver/static_test.go` | SPA 兜底、缓存头、API 404 返回 JSON、目录不完整时启动失败 |
| `httpserver/router_test.go` | **公开端点集合的 fail-closed 断言**、**装配遗漏检测**、未登录一律 401、登录/登出全链路、停用用户立刻失效、响应信封、错误键、字段级校验数组、413/404 映射 |
| `httpserver/rbac_api_test.go` | 权限隔离（有码放行、无码 403）、授权按码精确生效、未知权限码拒绝、409 影响面与 cascade 确认、四条删除守卫、唯一冲突带字段名、重置密码踢会话 |
| `middleware/middleware_test.go` | 限流按 IP、伪造 `X-Forwarded-For` 无效、请求体上限、缓存头只匹配前缀 |
| `system/system_test.go` | `/proc/stat` / `meminfo` / `statm` / `loadavg` 的解析（用真实样本）、`guest` 不计两次、单位与不可用标记、非 Linux 平台自报不可用 |
| `httpserver/monitor_test.go` | 会话列表与踢人、不能踢自己、**强退不存在的用户返回 404**、登录日志记录成败与来源、操作日志记录写操作与 403、**操作日志不含请求体** |
| `service/job_log_test.go` | 每次执行（成功 / 失败 / 跳过）都留一条记录与触发方式、**两条写库是同一个事务**（第二条失败时 `last_run_at` 不跟着变）、日志按保留期清理且不误删新记录 |
| `httpserver/job_logs_test.go` | 执行历史的三层授权（401 / 403 / `tool:job:list` 放行）、**触发一次后历史里真出现那一条**、未注册的 key 返回 404、页码越界钳到最后一页 |
| `httpserver/fixture_test.go` | 不是用例，是被各测试复用的夹具（临时库 + 完整路由） |
| `httpserver/router_test.go` 的 `TestPanicReturnsEnvelope` | **panic 也要返回信封**——空 body 的 500 会让前端把「后端出错」误判成「后端没起来」 |

### ⚠️ 三个跨语言用例必须用 `-count=1`

`perm/perm_test.go`、`httpx/response_test.go` 与 `repository/menu_seed_test.go` 里各有一个用例会去读前端源码
（i18n 字典，以及 `web/src/pages/` 下的页面文件），以确认「后端出的键」「菜单种子指向的页面」
与「前端真实存在的文件」没跑偏。

**Go 的测试缓存不会追踪测试运行期 `os.ReadFile` 打开的文件**，而这三个用例的输入恰好
都在包目录之外。不加 `-count=1` 时，改了前端字典再跑 `go test` 会拿到缓存里那个已经
过期的 `ok`——实测确认过：

```
$ go test ./internal/perm/        # 删掉字典里的一个键之后
ok  mini-ruoyi/internal/perm  (cached)     ← 假的通过
$ go test ./internal/perm/ -count=1
--- FAIL: TestFrontendDictCoversPermKeys
    缺少文案键 "perm.system.user.resetPwd"
```

CI 是冷缓存所以不受影响；本地请用 `make test` 或 `make check`。

写测试时注意：**gin 的校验器是全局单例**，`TestMain` 之外不要依赖注册顺序；
测试里的 `newXxxFixture` 记得跑 `repository.Migrate`，否则表不存在会得到一堆 500。

## 10. 新增一个资源的完整步骤

这一节是「照抄即可」的清单。一个普通资源要动 **12 个文件**（后端 8 + 前端 3 + 菜单种子迁移 1），
外加 3 个测试文件；每一步都有参照物，**金标准是 `role`（角色）**——最标准的「单表 + 分页列表 + 增删改」，
没有上传、密码、树形这些特例。

| 要写的东西 | 照抄 | 不要抄 |
| --- | --- | --- |
| 实体 | `domain/rbac.go` 的 `Role` | 新资源单独一个 `domain/<资源>.go`（只有同族实体才并进已有文件） |
| 建表 | `migrations/0002_init_rbac.sql` 的 `sys_roles` | — |
| 仓储 | `repository/role_repository.go` | `Grants` / `ReplaceGrants` 等授权专用方法 |
| 服务 | `service/role_service.go` | `Grants` / `SetGrants` |
| 处理器 | `handler/role_handler.go` | `Grants` / `SetGrants`（`cascadeRequested` 留着，删除确认要用） |
| 前端页面 | `web/src/pages/system/roles.svelte` | 授权那个 tab 整段 |

以新增 `widget` 为例：

1. **实体** — `domain/widget.go`：字段 + JSON tag（字段名就是对外的契约，见 §5.2）
2. **建表** — `repository/migrations/0013_init_widgets.sql`：现有最大编号是 `0012`，迁移只能往后加
3. **仓储** — `repository/widget_repository.go`：`List` / `Count` / `GetByID` / `Create` / `Update` / `Delete`，
   判空返回 `domain.ErrNotFound`，唯一性用 `ExistsXxx` 预检
4. **服务** — `service/widget_service.go`：分页归一化成 `Page[T]`、唯一冲突返回 `domain.Duplicate("字段名")`、
   删除前用 `Impact` 算影响面（有依赖就 `domain.HasDependents`）
5. **处理器** — `handler/widget_handler.go`：绑定 + 校验 + 调 service，错误交给 `httpx.FailBindError` / `FailFromError`。
   校验规则写在请求结构体的 `binding` tag 上（`binding:"required,min=2,max=64"`），
   `FailBindError` 会把它展开成字段级数组，不用手写错误处理
6. **权限码** — `perm/perm.go`：声明 `SystemWidgetList/Add/Edit/Delete` 并加进 `Groups()`。
   每个码必须恰好属于一个分组，否则权限界面看不到它
7. **路由** — `httpserver/router.go`：`reg.protect(http.MethodGet, "/widgets", perm.SystemWidgetList, deps.Widget.List)`。
   **权限码是必填参数**，漏了编译不过；也不要把业务端点注册成公开端点——公开集合被测试钉死
8. **依赖组装** — `cmd/server/main.go`：`NewWidgetRepository` → `NewWidgetService` → `NewWidgetHandler`，再塞进 `deps`
9. **文案** — `web/src/lib/i18n/zh-CN.ts` + `en-US.ts` 各加三样：权限码文案
   （Go 按 `system:widget:add → perm.system.widget.add` 推导）、页面文案、菜单标题键 `menu.tool.widget`
10. **菜单种子** — 新加一条迁移（照抄 `0009_seed_tool_menu.sql`）：目录用 `INSERT ... SELECT ... WHERE NOT EXISTS` 保证幂等，
    子菜单靠 `title_key` 反查 `parent_id`。**漏了这步，页面在界面上根本点不进去**
11. **前端页面** — `web/src/pages/tool/widgets.svelte`：`component` 必须与第 10 步的种子一致（相对 `src/pages`、不带扩展名）
12. **测试** — `repository`（仓储 + 种子）、`service`（业务规则，样板 `newJobFixture`）、
    `httpserver`（端到端与三层授权，样板 `testDeps`）各加一个

**漏了哪一步，谁拦住你**

| 漏了 | 拦住它的东西 |
| --- | --- |
| 权限码没声明，或路由没传 | 编译不过 / 启动 panic（fail-closed） |
| 权限码没进 `Groups()` | `TestGroupsCoverAllCodesExactlyOnce` |
| 前端缺 `perm.*` 文案 | `TestFrontendDictCoversPermKeys` |
| 菜单种子漏了或名字写歪 | `TestMenuSeedResolvesToFrontend`（双向：也查有没有页面谁都点不进去） |
| 建表 SQL 与迁移记录对不上 | 启动时的结构校验 `verifySchema` |
| 资源没装配进 `deps` | `TestRouterRejectsIncompleteDeps` |

最后跑 `make check`（gofmt + vet + 交叉编译 + 全部后端测试 + 前端类型检查），
再 `make test-e2e` 真的点一遍页面。
