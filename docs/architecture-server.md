# 后端架构

Go 1.25 + Gin 1.10 + `database/sql` + SQLite（`modernc.org/sqlite`，纯 Go 无 CGO）。零 ORM、零额外服务。

## 1. 目录结构

```
server/
├── go.mod                          # module mini-ruoyi
├── data.db                         # 随仓库分发的初始数据库
├── cmd/server/main.go              # 启动、依赖组装、优雅关闭
└── internal/
    ├── config/config.go            # 环境变量 → Config
    ├── domain/                     # 实体 + 领域错误（最内层，不依赖任何包）
    │   └── device.go
    ├── httpx/response.go           # 统一响应信封、错误键、错误→HTTP 状态码映射
    ├── repository/
    │   ├── sqlite.go               # 连接与连接池
    │   ├── migrate.go              # 迁移执行器
    │   ├── migrations/*.sql        # 版本化 DDL
    │   └── device_repository.go    # 单表 CRUD
    ├── service/device_service.go   # 业务规则、事务边界、分页归一化
    ├── handler/device_handler.go   # HTTP 适配：绑定、校验、响应
    ├── middleware/middleware.go    # 日志、限流、请求体上限、静态资源缓存头
    └── httpserver/
        ├── router.go               # 路由注册、中间件装配
        └── static.go               # 磁盘静态托管 + SPA 兜底
```

## 2. 分层与依赖方向

依赖严格单向，无环：

```
domain      →（不依赖任何内部包）
httpx       → domain
repository  → domain
service     → domain, repository
handler     → httpx, service
middleware  → httpx
httpserver  → handler, httpx, middleware
```

| 层 | 职责 | 禁止 |
| --- | --- | --- |
| `domain` | 实体定义、领域错误哨兵（`ErrNotFound`） | 依赖任何其他内部包 |
| `httpx` | 响应信封、错误键、错误→状态码映射 | 依赖 handler / service / repository |
| `repository` | 单表 SQL，行 ↔ 实体 | 包含业务规则、跨表事务编排 |
| `service` | 业务规则、事务边界、分页归一化 | 依赖 `*gin.Context`、构造 HTTP 响应 |
| `handler` | 参数绑定、调用 service、写响应 | **依赖 `repository`**（靠 `domain` 的错误哨兵判错） |
| `middleware` | 横切关注点 | 依赖 handler |
| `httpserver` | 路由与中间件装配、静态资源 | 包含业务逻辑 |

两条硬规则：

1. **handler 不得 import repository。** 判错统一用 `domain.ErrNotFound`，映射集中在
   `httpx.FailFromError` 一处。历史上这里是跨层泄漏，已修正。
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
1. gin.Recovery()                                   panic → 500
2. middleware.RequestLogger()                       方法 路径 状态码 耗时
3. SetTrustedProxies(nil)                           ← 不是中间件，是路由配置，但顺序关键
4. middleware.BodyLimit(1 MiB)                      请求体上限
5. middleware.RateLimit(20 rps, burst 40, TTL 3m)   按 IP 限流
6. middleware.ImmutableAssets("/assets/")           静态资源长缓存头
   ── 路由 ──
   GET  /healthz
   /api/v1/devices  POST / GET / GET :id / PATCH :id / DELETE :id
   /assets/*filepath
   ── NoRoute ──
   /api/* 与 /assets/* → JSON 404；其余 → index.html（SPA 兜底）
```

### 4.1 两个容易踩的坑

**限流必须按 IP，不能全进程。** 全进程令牌桶意味着单个客户端刷满配额后，其他所有用户
一起收到 429——限流器反而成了 DoS 放大器。同时限流器 map 必须惰性 GC，
否则被扫描时 map 会随访问过的 IP 无上限增长。

**不能信任 `X-Forwarded-For`。** 本服务直接对外监听，没有 nginx。若不调用
`SetTrustedProxies(nil)`，gin 的 `ClientIP()` 会采信客户端自带的 `X-Forwarded-For`，
限流可以被一行 header 绕过。测试用例 `TestRateLimitIgnoresSpoofedForwardedFor` 覆盖这一点。

## 5. 响应信封（`internal/httpx`）

```go
type Response struct {
    Code   int          `json:"code"`             // 0 成功 / 1 失败
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
否则前端拿到 `data.list === null` 会崩。

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

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `APP_ADDR` | `:8080` | 监听地址 |
| `APP_DB_PATH` | `data.db` | 相对**进程工作目录** |
| `APP_WEB_DIR` | 二进制同级 `web/`，不存在则 `./web` | 前端产物目录 |
| `APP_ENV` | `prod` | `dev` → `gin.DebugMode` |

`APP_WEB_DIR` 的默认值跟二进制走（`os.Executable()`），这样部署时二进制和 `web/` 放一起即可，
不依赖 systemd 的 `WorkingDirectory`；目录不存在时回落到 CWD 相对路径，
覆盖 `go run` 的场景（`go run` 会把二进制放进临时目录，同级不会有 `web/`）。

## 9. 测试

```bash
cd server && go test ./...        # 全部
go test ./... -run TestNewDB      # 单个
```

| 文件 | 覆盖 |
| --- | --- |
| `repository/sqlite_test.go` | **每条连接的 PRAGMA 都生效**（防 A1 回归）、WAL 已启用 |
| `repository/migrate_test.go` | 迁移幂等；在旧版遗留库（有表无迁移记录）上升级不丢数据 |
| `repository/device_repository_test.go` | `created_at` 回填、空列表非 nil、分页、`ErrNotFound` |
| `handler/device_handler_test.go` | `PATCH {}` 不能静默改 `enabled`、空列表 `[]`、snake_case 字段名 |
| `httpserver/static_test.go` | SPA 兜底、缓存头、API 404 返回 JSON、目录不完整时启动失败 |
| `httpserver/router_test.go` | 响应信封、错误键、字段级校验数组、413、404 映射 |
| `middleware/middleware_test.go` | 限流按 IP、伪造 `X-Forwarded-For` 无效、请求体上限、缓存头只匹配前缀 |

写测试时注意：**gin 的校验器是全局单例**，`TestMain` 之外不要依赖注册顺序；
测试里的 `newXxxFixture` 记得跑 `repository.Migrate`，否则表不存在会得到一堆 500。

## 10. 新增一个资源的完整步骤

以新增 `widget` 为例：

1. `domain/widget.go` — 实体 + JSON tag
2. `repository/migrations/0002_init_widgets.sql` — 建表 DDL
3. `repository/widget_repository.go` — 单表 CRUD，判空返回 `domain.ErrNotFound`
4. `service/widget_service.go` — 业务规则、分页归一化
5. `handler/widget_handler.go` — 绑定 + 校验 + 调 service，错误交给 `httpx.FailBindError` / `FailFromError`
6. `httpserver/router.go` — 注册路由
7. `main.go` — 组装依赖
8. 前端：`web/src/pages/` 加页面、`zh-CN.ts` / `en-US.ts` 加文案

**要点**：校验规则写在请求结构体的 `binding` tag 上（`binding:"required,min=2,max=64"`）；
失败时用 `httpx.FailBindError`，它会自动展开成字段级数组，不用手写错误处理。
