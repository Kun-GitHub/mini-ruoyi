# mini-ruoyi / server

后端服务。Go 1.25 + Gin + `database/sql` + SQLite（`modernc.org/sqlite`，纯 Go 无 CGO）。

模块名 `mini-ruoyi`。数据库后端只有 SQLite 一种，没有多方言抽象。

## 前置要求

- Go 1.25 或更高（`go.mod` 声明 `go 1.25`，启用了 `GOTOOLCHAIN=auto` 时会自动下载对应工具链）
- 不需要 CGO，不需要 SQLite 命令行工具

## 快速开始

```bash
cd server

# 直接跑（若当前目录或二进制同级目录没有 web/，会自动降级为纯 API 模式）
go run ./cmd/server

# 验证
curl localhost:8080/healthz
# {"code":0,"msg":"ok","data":{"status":"up"}}
```

从仓库根目录构建完整产物（含前端）：

```bash
make build && make run       # 二进制与前端产物落在 bin/
```

启动时会自动执行未应用的迁移，并创建 `data.db`（若不存在）。
仓库自带的 `data.db` 是一个空的初始库，开箱即用。

## 配置

**优先级：环境变量 > 配置文件 > 内置默认值。**

```bash
cp config/config.example.yaml config/config.yaml   # 可选，不改也能跑
```

`config.yaml` 在 `.gitignore` 里；可提交的样例是 `config/config.example.yaml`，
每一项都带注释。用 `APP_CONFIG` 可以指定别的路径。

| 变量 | 默认值 | 配置文件里的位置 |
| --- | --- | --- |
| `APP_CONFIG` | `config/config.yaml` | —（配置文件路径本身） |
| `APP_ADDR` | `:8080` | `server.addr` |
| `APP_ENV` | `prod` | `server.env` |
| `APP_SECURE_COOKIE` | `false` | `server.secure_cookie` |
| `APP_TRUSTED_PROXIES` | 空 | `server.trusted_proxies`（逗号分隔） |
| `APP_RATE_LIMIT_RPS` | `20` | `server.rate_limit.rps` |
| `APP_RATE_LIMIT_BURST` | `40` | `server.rate_limit.burst` |
| `APP_DB_PATH` | `data.db` | `database.path` |
| `APP_WEB_DIR` | 自动查找 | `web.dir` |
| `APP_UPLOAD_DIR` | `uploads` | `upload.dir` |
| `APP_UPLOAD_MAX_MB` | `20` | `upload.max_mb` |
| `APP_UPLOAD_QUOTA_MB` | `512` | `upload.quota_mb` |
| `APP_LOG_RETENTION_DAYS` | `30` | `log.retention_days` |

**配置错了会拒绝启动**：配置文件里出现未知字段、环境变量取值非法、
以及「配额小于单文件上限」这类自相矛盾的组合，都会在启动时失败并指出具体位置。
静默退回默认值的话，用户只会觉得「我明明配了怎么不生效」。

启动日志里有一行生效配置摘要，路径都是绝对路径。

## API

全部在 `/api/v1` 下。完整契约（信封、错误键、删除确认）见
[../docs/architecture.md](../docs/architecture.md)。

启动时会打印当前端点分类：

```
已注册 23 个 API 端点（公开 1 / 仅登录 2 / 需权限 20）
```

**认证方式**：登录后服务端下发 `mr_session` cookie（HttpOnly / SameSite=Lax）。
除登录外，所有写操作都必须带 `X-CSRF-Token` 头，值取自登录响应或 `/auth/me` 的
`csrf_token` 字段。

### 端点一览

| 方法 | 路径 | 权限码 | 说明 |
| --- | --- | --- | --- |
| POST | `/auth/login` | — **(公开)** | 登录，下发 cookie 与 CSRF 令牌 |
| GET | `/auth/me` | 仅登录 | 当前用户 + 权限码 + 菜单树 + CSRF 令牌 |
| POST | `/auth/logout` | 仅登录 | 登出，服务端立即删除会话 |
| PUT | `/profile` | 仅登录 | 改**自己**的昵称/手机/邮箱。改不了状态——否则等于允许自解禁 |
| PUT | `/profile/password` | 仅登录 | 改**自己**的密码。需提供旧密码，只踢掉其他会话 |
| GET | `/menus` | `system:menu:list` | 完整菜单树（含未启用项） |
| GET | `/menus/:id` | `system:menu:list` | |
| POST | `/menus` | `system:menu:add` | |
| PUT | `/menus/:id` | `system:menu:edit` | |
| DELETE | `/menus/:id` | `system:menu:delete` | 支持 `?cascade=true` |
| GET | `/roles` | `system:role:list` | 分页 |
| GET | `/roles/:id` | `system:role:list` | |
| GET | `/roles/:id/grants` | `system:role:list` | 菜单授权 + 权限码 |
| POST | `/roles` | `system:role:add` | |
| PUT | `/roles/:id` | `system:role:edit` | |
| PUT | `/roles/:id/grants` | `system:role:edit` | 覆盖式重设授权 |
| DELETE | `/roles/:id` | `system:role:delete` | 支持 `?cascade=true` |
| GET | `/perms` | `system:perm:list` | 权限清单，按资源分组，**含每个权限码保护的接口**（来自启动时装配的路由表，不查库） |
| GET | `/users` | `system:user:list` | 分页 |
| GET | `/users/:id` | `system:user:list` | 含 `role_ids` |
| POST | `/users` | `system:user:add` | |
| PUT | `/users/:id` | `system:user:edit` | 用户名不可变 |
| PUT | `/users/:id/roles` | `system:user:edit` | |
| PUT | `/users/:id/password` | `system:user:resetPwd` | 重置后**立即踢掉该用户全部会话** |
| DELETE | `/users/:id` | `system:user:delete` | |
| GET | `/system` | `monitor:system:list` | 本机 CPU / 内存 / 磁盘 / Go 进程状态 |
| GET | `/sessions` | `monitor:session:list` | 在线会话（未过期的），最近活跃在前 |
| DELETE | `/sessions/:hash` | `monitor:session:kick` | 踢掉一条会话，对方下次请求即失效 |
| DELETE | `/users/:id/sessions` | `monitor:session:kick` | 强退某用户全部会话 |
| GET | `/login-logs` | `monitor:loginlog:list` | 登录日志，支持 `username` / `status` 筛选 |
| GET | `/oper-logs` | `monitor:operlog:list` | 操作日志，支持 `username` / `method` / `path` 筛选 |
| GET | `/files` | `tool:file:list` | 文件列表，响应还带容量用量 |
| GET | `/files/:id/download` | `tool:file:list` | 下载。**强制保存**，不做内联预览 |
| POST | `/files` | `tool:file:upload` | multipart 上传，字段名 `file`，可带 `group` |
| DELETE | `/files/:id` | `tool:file:delete` | 删除记录与磁盘文件 |
| GET | `/jobs` | `tool:job:list` | 定时任务（来自代码注册表） |
| PUT | `/jobs/:key` | `tool:job:edit` | 改 cron / 启停，**立即重新调度** |
| POST | `/jobs/:key/run` | `tool:job:run` | 立即执行一次（异步，结果刷新列表看） |
| GET | `/healthz` | — **(公开)** | 探活，会真实 Ping 数据库 |

### 「自己」与「他人」是两组接口

| | 改自己的 | 改他人的 |
| --- | --- | --- |
| 资料 | `PUT /profile`（仅登录） | `PUT /users/:id`（要 `system:user:edit`） |
| 密码 | `PUT /profile/password`（仅登录，**需旧密码**） | `PUT /users/:id/password`（要 `system:user:resetPwd`） |

改自己的那两个**不要任何权限码**：初始密码是管理员设的，如果改自己的密码也要
`system:user:resetPwd`，那一个只授了只读权限的账号连自己的密码都改不了。
它们走的是 `self` 路由（仅校验登录）。

两处差异是刻意的：

- 改自己密码**必须验旧密码**——否则一个被盗用的会话就能直接改掉密码把机主锁在外面
- 改自己密码**只踢其他会话**，当前这条保留。改完立刻把自己登出，用户会以为改失败了

### 登录

```bash
curl -c cookie.txt -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'
```

```json
{"code":0,"msg":"ok","data":{
  "user":{"id":1,"username":"admin","nickname":"管理员", ...},
  "is_admin":true,
  "perms":["system:menu:add", "..."],
  "csrf_token":"Xce5DgS3...",
  "expires_at":"2026-10-04T08:45:15Z",
  "menus":[{"title_key":"menu.system","children":[...]}]
}}
```

内置管理员（角色 `code='admin'`）的 `perms` 会返回**全部已声明的权限码**，
所以前端只需要一种判断：`perms.includes(code)`。它的权限是隐式的、不落库，
见 [../docs/schema.md](../docs/schema.md) §8.2。

### 删除有依赖的资源

不带 `?cascade=true` 时，若资源有子数据会返回 `409` 并携带影响面：

```bash
curl -b cookie.txt -X DELETE localhost:8080/api/v1/menus/1
```

```json
{"code":1,"msg":"error.hasDependents",
 "data":{"child_menus":3,"affected_roles":2}}
```

前端收到它**不是弹错误提示，而是弹确认框**并把数字展示出来；用户确认后重发
`DELETE .../menus/1?cascade=true`。各资源的影响面字段见
[../docs/architecture.md](../docs/architecture.md) §3.4。

### 错误响应

失败响应的 `msg` 是 **i18n 键**而非文案，文案由前端渲染。

| HTTP | `msg` |
| --- | --- |
| 400 | `error.validationFailed` / `error.malformedBody` / `error.invalidId` / `error.invalidParent` / `error.invalidPermCode` |
| 401 | `error.unauthorized` / `error.badCredentials` |
| 403 | `error.forbidden` / `error.protected` / `error.accountDisabled` / `error.cannotDeleteSelf` / `error.cannotKickSelf` / `error.csrfInvalid` |
| 404 | `error.notFound` |
| 409 | `error.hasDependents` / `error.lastAdmin` / `error.duplicate` |
| 400 | `error.invalidJobCron` / `error.invalidFile` / `error.wrongOldPassword` |
| 413 | `error.bodyTooLarge` / `error.fileTooLarge` / `error.quotaExceeded` |
| 429 | `error.tooManyRequests` |
| 500 | `error.internal` |
| 503 | `error.serviceUnavailable` |
| 404 | `error.frontendDisabled`（**纯 API 模式**下访问非 API 路径，提示前端未部署） |

后端**不会**返回 `error.network` 和 `error.backendUnreachable`——这两个是前端产生的
（前者表示请求没有任何东西应答，后者表示有响应但不是本服务的信封格式）。

内置限制：请求体上限 1 MiB，按 IP 限流 20 rps（突发 40）。
审计日志批量落库，**最多有 2 秒延迟**（原因见 [../docs/architecture.md](../docs/architecture.md) §5.2）。
保留期由 `APP_LOG_RETENTION_DAYS` 控制，默认 30 天。

## 数据库

### 加一个迁移

1. 在 `internal/repository/migrations/` 新建 `<序号>_<描述>.sql`，序号零填充 4 位：

   ```
   internal/repository/migrations/0002_init_users.sql
   ```

2. 直接写 SQL，一个文件可以有多条语句：

   ```sql
   CREATE TABLE users (
       id       INTEGER PRIMARY KEY AUTOINCREMENT,
       username TEXT NOT NULL UNIQUE,
       ...
   );
   CREATE INDEX idx_users_username ON users(username);
   ```

3. 重启服务即自动应用。

规则：

- **只向前补齐，不支持回滚。** 改错了就再加一个修复迁移
- 每个文件在**单个事务**内执行，DDL 失败不会留下半截结构
- 执行记录写入 `schema_migrations`，已应用的会跳过。重复启动是幂等的
- 迁移文件通过 `//go:embed` 打包进二进制，因此**必须与代码一起发布**，不能靠运维单独拷 SQL

### 改表结构的注意事项

仓库里的 `data.db` 是随代码分发的旧库，可能没有迁移记录。因此**首个迁移要能容忍表已存在**
（用 `IF NOT EXISTS`），否则老库升级会失败。参考
`migrations/0001_init_devices.sql` 与用例 `TestMigrateOnLegacyDatabase`。

### SQLite 连接配置

`PRAGMA` 通过 DSN 的 `_pragma` 参数下发，**不要改成 `db.Exec("PRAGMA ...")`**——
那是连接级设置，`db.Exec` 只会配置连接池里的某一条连接，其余连接仍是默认值
（`foreign_keys=0`、`busy_timeout=0`）。详见
[../docs/architecture-server.md](../docs/architecture-server.md)。

`repository/sqlite_test.go` 有对应回归用例，改动连接配置后必须让它通过。

## 测试

```bash
go test ./...                                    # 全部
go test ./internal/repository/ -v                # 单包
go test ./... -run TestNewDBConfiguresPragmas    # 单例
gofmt -l . && go vet ./...                       # 提交前
```

用例覆盖范围见 [../docs/architecture-server.md](../docs/architecture-server.md) 的「测试」一节。

## 代码导航

```
cmd/server/main.go                 启动、依赖组装、优雅关闭
internal/config/                   环境变量 → Config
internal/domain/                   实体 + 领域错误（最内层）
internal/httpx/                    响应信封、错误键、错误→状态码映射
internal/repository/               连接、迁移、单表 SQL
internal/service/                  业务规则、分页归一化
internal/handler/                  HTTP 适配：绑定、校验、响应
internal/middleware/               日志、限流、请求体上限、缓存头
internal/httpserver/               路由装配、静态资源托管 + SPA 兜底
```

依赖方向严格单向，`handler` 不得 import `repository`。详见
[../docs/architecture-server.md](../docs/architecture-server.md)。

新增一个资源的完整步骤（8 步）也写在那份文档里。

## 尚未实现

- **登录失败锁定**：现在只记登录日志，不累计失败次数、不锁账号
- **任务执行历史**：`sys_jobs` 上只存最近一次执行结果，没有历史表
- **列表导出**：用户/角色/日志都没有导出接口

`devices` 示例资源已移除：在 fail-closed 的权限模型下，它要么污染生产权限清单，
要么留下一个无权限的 CRUD 接口，两者都不该出现在正式版本里。
