# 整体架构

[English](architecture.en.md) | 简体中文

本文描述 mini-ruoyi 的系统全貌、前后端契约与关键决策。分层细节见
[architecture-server.md](architecture-server.md) 和 [architecture-web.md](architecture-web.md)。

## 1. 系统全貌

只有一个进程，没有反向代理，没有独立数据库服务，没有缓存服务。

```
                        ┌─────────────────────────────────────┐
   浏览器                │  mini-ruoyi (单个 Go 进程)           │
   ────────────────────▶│                                     │
     :8080              │  Gin 路由                           │
                        │   ├── /healthz        ──▶ SQLite Ping│
                        │   ├── /api/v1/*       ──▶ 业务逻辑   │
                        │   ├── /assets/*       ──▶ 磁盘文件   │
                        │   └── 其余 ──▶ index.html (SPA 兜底) │
                        │                                     │
                        │         SQLite (WAL)                │
                        └──────────────┬──────────────────────┘
                                       ▼
                              data.db / data.db-wal
```

部署目录：

```
/opt/mini-ruoyi/
├── mini-ruoyi        # 二进制（含全部后端逻辑）
├── web/              # 前端产物：index.html + assets/
└── data.db           # SQLite 数据库
```

前端产物**不在二进制里**。这是一个有意的取舍，理由见 §5。

## 2. 构建流水线

```
web/src/**  ──[vite build]──▶  web/dist/  ──[cp -R]──▶  bin/web/
                                                            │
server/**   ──[go build]────────────────────▶  bin/mini-ruoyi
                                                            │
                                            bin/  ──[scp]──▶  服务器
```

`make build` 一条命令完成前四步中的构建部分（构建前端、拷贝产物、编译二进制），
最后一步 scp 由你按自己的发布流程做。开发时用 `make dev-server` + `make dev-web`，
Vite dev server 把 `/api` 与 `/healthz` 代理到 `:8080`，前端热更新与后端互不干扰。

## 3. 前后端契约

### 3.1 响应信封

**所有** JSON 接口共用同一个信封，包括错误：

```jsonc
{
  "code": 200,                     // 恒等于 HTTP 状态码
  "msg":  "ok",                    // 成功为 "ok"；失败为 i18n 键（见 §6）
  "data": { },                     // 只有成功时存在
  "errors": [ ]                    // 只有字段级校验失败时存在
}
```

**语义由 HTTP 状态码承载**，而 `code` 恒等于同一个状态码（`write()` 统一写入，调用方碰不到它）。
这是相对若依原版（HTTP 恒 200 + `code:200/500`）的一个有意偏离——保留 HTTP 语义后，
curl、日志、监控、网关都能直接看出错误率，不用解析 body。

**为什么 `code` 是状态码的副本，而不是 0/1：** Go 的 `int` 零值是 0，而 `Code` 字段
没有 `omitempty`，所以拿 0 当成功值时，任何「忘了给 `Code` 赋值」的新代码路径
都会**静默返回成功**。实测：

```go
json.Marshal(Response{Msg: "ok", Data: x})   // 忘了设 Code
→ {"code":0,"msg":"ok",...}                // 前端判成成功
```

取状态码做值，零值 0 就**不可能是合法值**——忘了赋值会当场露出破绽。

`code` 的唯一消费方是**信封判别器**（见 [architecture-web.md](architecture-web.md) §5.4）：
前端据此区分「这是我家的响应」与「中间有代理应答」。成功与否看 HTTP 状态，不看 `code`。

| 场景 | HTTP | `msg` |
| --- | --- | --- |
| 成功 | 200 | `ok` |
| 参数校验失败 | 400 | `error.validationFailed` |
| 请求体格式错误 | 400 | `error.malformedBody` |
| 路径参数非法 | 400 | `error.invalidId` |
| 上级节点不合法 | 400 | `error.invalidParent` |
| 资源不存在 | 404 | `error.notFound` |
| 未登录 / 会话失效 | 401 | `error.unauthorized` |
| 用户名或密码错误 | 401 | `error.badCredentials` |
| 内置资源不可改/删 | 403 | `error.protected` |
| 账号已停用 | 403 | `error.accountDisabled` |
| 已登录但缺少权限 | 403 | `error.forbidden` |
| CSRF 令牌缺失或不匹配 | 403 | `error.csrfInvalid` |
| 不能删除当前登录账号 | 403 | `error.cannotDeleteSelf` |
| 存在关联数据，需确认 | 409 | `error.hasDependents`（响应体带影响面，见 §3.4） |
| 不能删除/停用最后一个管理员 | 409 | `error.lastAdmin` |
| 唯一字段冲突 | 409 | `error.duplicate`（`errors[]` 带冲突字段名） |
| 提交了未知权限码 | 400 | `error.invalidPermCode` |
| 响应不是本服务的信封 | 任意 | `error.backendUnreachable`（**前端产生**：请求没到后端，见 [architecture-web.md](architecture-web.md) §5.4） |
| 请求体过大 | 413 | `error.bodyTooLarge` |
| 触发限流 | 429 | `error.tooManyRequests` |
| 服务不可用 | 503 | `error.serviceUnavailable` |
| 服务器内部错误 | 500 | `error.internal` |
| 旧密码不正确 | 400 | `error.wrongOldPassword` |
| 上传的文件本身不合法（如空文件） | 400 | `error.invalidFile` |
| cron 表达式无法解析 | 400 | `error.invalidJobCron` |
| 不能踢掉自己当前这条会话 | 403 | `error.cannotKickSelf` |
| 单个文件超过上限 | 413 | `error.fileTooLarge` |
| 超出总容量配额 | 413 | `error.quotaExceeded` |

完整的键清单在 §6（含只由前端产生的两个）。

### 3.2 字段级校验失败

后端只给「哪个字段、违反哪条规则、规则参数是多少」，**不给文案**：

```json
{
  "code": 400,
  "msg": "error.validationFailed",
  "errors": [
    { "field": "name",     "rule": "min",      "param": "2" },
    { "field": "location", "rule": "required" }
  ]
}
```

- `field` 是 **JSON 字段名**（`name`），不是 Go 字段名（`Name`）
- `param` 在规则无参数时省略
- 文案由前端用 `rule` + `param` + 字段标签拼装，见 [architecture-web.md](architecture-web.md)

### 3.3 契约不变式

写新接口时必须维持：

1. 响应体永远有 `code` 和 `msg`，且 `code` 恒等于 HTTP 状态码（由 `httpx.write()` 保证）
2. 失败响应的 `msg` 永远是 i18n 键，**不含自然语言**（否则前端无法翻译）
3. `data.list` 在空结果时是 `[]` 而非 `null`（否则前端要特判）
4. 列表接口的分页结构固定为 `{list, total, page, page_size}`，且：
   - `page_size` 小于 1 时取默认 20，**大于上限 100 时钳到 100**（不是悄悄换成 20——
     要 101 条却拿到 20 条，调用方会以为数据只有这么多）
   - 响应里的 `page` 是**实际使用的页码**，越界时已钳到最后一页。前端以它为准，
     否则一个过期的书签会让分页器显示「99 / 2」且表格空白
5. 时间字段输出 RFC 3339 UTC（Go `time.Time` 的默认 JSON 序列化）

### 3.4 删除有依赖的资源

资源带有子数据时，删除不能被无条件执行。契约是：**服务端在删除时当场算影响面，
有依赖则返回 `409` 并携带影响面，前端据此弹框，确认后带 `?cascade=true` 重发。**

```
DELETE /api/v1/menus/5
→ 409 Conflict
  {"code":409,"msg":"error.hasDependents",
   "data":{"child_menus":3,"affected_roles":2}}

DELETE /api/v1/menus/5?cascade=true
→ 200 {"code":200,"msg":"ok","data":{"id":5}}
```

各资源的影响面：

| 资源 | 影响面字段 | 含义 |
| --- | --- | --- |
| 菜单 | `child_menus` | 后代菜单数（级联删除会一并消失） |
| 菜单 | `affected_roles` | 引用了该菜单或其任一后代的角色数 |
| 角色 | `affected_users` | 持有该角色的用户数 |
| 用户 | — | 不拦截。角色关联是用户自身的附属数据，删除即失效是预期行为 |

**为什么用「409 + 重发」而不是单独的预检接口：**

- 检查与删除在同一次请求里，没有 TOCTOU 窗口。若用 `GET .../deletion-impact` 预检，
  在弹框与确认之间另一个管理员新增了子节点，用户会在不知情的情况下多删数据——
  而“不让用户意外多删”正是这个需求的目的
- 无依赖时（绝大多数情况）只需一次往返

**前端必须遵守的一条规则：** `409` + `msg == "error.hasDependents"` **不是错误**，
不能弹错误提示，而应直接弹出确认框并把 `data` 里的数字展示出来。

新增错误键：`error.hasDependents`（需同步 `web/src/lib/i18n/`）。

## 4. 缓存策略

```
/index.html      Cache-Control: no-cache                       每次回源校验
/assets/*        Cache-Control: public, max-age=31536000, immutable   内容哈希，永久缓存
/api/*           不缓存
```

`index.html` **必须** `no-cache`。Vite 产物的 asset 文件名带内容哈希，`index.html` 不带；
如果 `index.html` 被缓存，用户刷新时会继续引用已不存在的旧哈希，前端就永远更新不了。
这条是"重新构建前端后刷新即最新"这个能力成立的前提。

## 5. 关键决策与权衡

| 决策 | 备选 | 选择理由 |
| --- | --- | --- |
| **单进程 + 磁盘托管静态资源** | 独立静态服务器 / nginx 反代 | 1C1G 上省掉一个常驻进程和一份配置；单机场景 nginx 只带来运维成本 |
| **前端产物不 embed 进二进制** | `go:embed` 打包成单文件 | 前端可独立更新，不需重启后端（`go:embed` 每次改前端都要重编译重启）。代价是部署单位从 1 个文件变成 1 个目录，以及二进制少 300KB 左右 |
| **只用 SQLite 单数据源** | 同时支持 MySQL + SQLite | 双数据源需要两套 DDL、两套方言 SQL、两倍测试矩阵、CI 起 MySQL。"极简"目标下这个成本换不来收益 |
| **`modernc.org/sqlite`** | `mattn/go-sqlite3`（CGO） | 纯 Go，`GOOS/GOARCH` 交叉编译无依赖。性能约 CGO 版一半，单机管理后台完全够用 |
| **不用 ORM** | GORM / Ent / sqlc | 管理后台 QPS 个位数，ORM 的反射与代码生成收益低；手写 SQL 便于排查，也符合"极简" |
| **HTTP 语义状态码** | 若依式 HTTP 恒 200 + `code:200/500` | 见 §3.1。`code` 是状态码的副本而非 0/1：Go 的 int 零值是 0，用 0 表示成功会让「忘了给 Code 赋值」静默变成成功 |
| **后端只出 i18n 键** | 后端按 `Accept-Language` 输出文案 | 文案只需存在一处（前端，UI 所在处）；切换语言无需重新请求；后端保持语言中立，多语言用户都能用 |
| **前端手写 i18n** | svelte-i18n / paraglide-js | 2 个语种、消息量小，运行时库的动态加载与格式化插件全用不上；手写版靠 TS 类型保证缺键编译报错 |
| **版本化迁移** | `CREATE TABLE IF NOT EXISTS` | 后者无法改表结构，等于"上线后手工改库" |
| **认证用 Cookie + 服务端 session** | 无状态 JWT | 单机单进程，JWT 的水平扩展优势为零，但"无法主动登出/踢人/权限变更不实时"的缺点一个不落。且若依本身的 JWT 也只是个壳，真实状态在 Redis——我们没有 Redis，session 表语义等价。落地见 [schema.md](schema.md) §3.7 |
| **不引入 Redis** | Redis 存 session | 单机上 Redis 只增加一个部署单元和一个故障点。同机 SQLite 主键查询（~1-3 µs）比 Redis over loopback（~30-60 µs）还快 |
| **带内容哈希的静态资源永久缓存** | 统一 `no-cache` | 省掉首屏之外的全部重复请求；哈希保证不会拿错版本 |

## 5.1 四种部署形态

前端产物是纯静态文件，由谁托管都可以。**后端在 `web/` 目录不存在时会降级成纯 API 模式**，
所以下面四种都能跑：

| 形态 | 前端托管 | 需要的配置 |
| --- | --- | --- |
| 1. 单二进制 | Go（`bin/web/`） | 无，默认 |
| 2. 本地调试 | Vite dev server（`/api` 代理到后端） | `APP_WEB_DIR` 指向不存在的位置即可（或直接不构建前端） |
| 3. nginx 托管前端 | nginx 静态目录 + `try_files` | `APP_TRUSTED_PROXIES=<代理地址>` |
| 4. nginx 只做 TLS 终止 | Go（`bin/web/`） | `APP_TRUSTED_PROXIES` + `APP_SECURE_COOKIE=true` |

**形态 2**（Vite 调试）：

```bash
make dev-server   # 后端 :8080，没有 bin/web 时会打印一行提示，接口照常可用
make dev-web      # Vite :5173，/api 与 /healthz 代理到 :8080
```

此时打开 `http://localhost:8080/` 会看到
`{"code":404,"msg":"error.frontendDisabled"}` —— **这是预期的**，它明确告诉你
「后端在纯 API 模式，前端没部署」，而不是让你以为服务坏了。

**形态 3**（nginx 托管前端）：

```nginx
server {
    listen 80;
    root /opt/mini-ruoyi/web;

    # 带内容哈希的资源可以永久缓存（与后端托管时的策略一致）
    location /assets/ {
        add_header Cache-Control "public, max-age=31536000, immutable";
        try_files $uri =404;
    }

    # index.html 必须回源校验，否则前端重新构建后用户刷新拿不到新版本
    location / {
        add_header Cache-Control "no-cache";
        try_files $uri /index.html;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

后端侧：

```bash
APP_TRUSTED_PROXIES=127.0.0.1   # 必填，见下
```

### ⚠️ 放在反向代理后面就必须配 `APP_TRUSTED_PROXIES`

它的默认值是**空**，也就是不信任任何 `X-Forwarded-For`。这是对的：本服务可以直接
对外监听，无条件采信这个头会让客户端随便伪装成任意 IP，按 IP 限流形同虚设。

但反过来，nginx 后面**必须**声明代理地址，否则所有请求的来源 IP 都是 `127.0.0.1`：

| 配置 | `login_ip` 记录 | 按 IP 限流的实际效果 |
| --- | --- | --- |
| 不配 | `127.0.0.1`（代理地址） | **退化成全局限流**：一个人刷满 20 rps，所有人一起被 429 |
| `APP_TRUSTED_PROXIES=127.0.0.1` | 真实客户端 IP | 每个 IP 各自计数 |

支持 IP 与 CIDR，逗号分隔：`APP_TRUSTED_PROXIES=127.0.0.1,10.0.0.0/8`。
配置非法时**启动即失败**并指出问题，不会静默忽略。

> 注意：即使配了可信代理，`X-Forwarded-For` 的**最左**值也可能被客户端伪造。
> 本服务用 gin 的默认策略（取最右侧非可信地址），配合「只信任你自己的代理」是安全的。

## 5.2 审计日志为什么不是每请求直写

`sys_login_logs` 与 `sys_oper_logs` **不**在中间件里直接写库，而是走
「内存 channel 缓冲 + 批量落库」（`service.LogService`）：

```
请求 → RecordLogin/RecordOper → channel（容量 1024）
                                    ↓
                      攒够 64 条或每 2 秒 → 一个事务批量插入
```

理由：**SQLite 是单写者**。每个请求都写一行日志，就是每个请求都去抢一次写锁
并付一次事务开销，所有请求会在写锁上排队。攒批之后 N 个请求只付一次代价。

三个配套决定：

| 决定 | 理由 |
| --- | --- |
| 缓冲满了**丢日志**并计数 | 这是审计功能，不该拖慢主流程。丢的记录数会在关闭时打日志 |
| 关闭时 `LogService.Stop()` | 不落掉缓冲区的话，最后几秒的日志会丢——而那恰恰是最可能出问题的时间段 |
| 保留期分批删（每次 2000 行，每小时检查） | 到期的行可能很多，一条大 DELETE 会长时间持有写锁让所有请求排队 |

**代价是日志最多有 2 秒延迟**。界面上已注明；写测试时要注意——
断言「日志里出现了某条记录」必须靠「重新加载 + 重试」，
Playwright 定位器的自动重试只重新求值 DOM，不会重新发请求。

保留期由 `APP_LOG_RETENTION_DAYS` 控制，默认 30 天。

**任务执行日志（`sys_job_logs`）走的是另一条路：同步写**，与 `sys_jobs.last_*` 在
同一个事务里（见 [schema.md](schema.md) §3.11）。任务一天只跑几次，攒批没有收益；
而且「列表说这次成功了、历史里却查不到」这种不一致不能接受。
所以执行历史没有 2 秒延迟，刷新即见。

## 5.3 上传文件的三个安全约束

| 约束 | 原因 |
| --- | --- |
| 磁盘名由服务端生成（随机 hex），**绝不用用户给的文件名** | 直接拿用户输入当路径就是路径穿越（`../../etc/passwd`） |
| 上传目录**不能静态挂载**，必须走 handler 鉴权后 `http.ServeContent` | 静态挂载会绕过鉴权，任何人拼一个 URL 就能拿到别人上传的东西 |
| 下载一律 `Content-Disposition: attachment` + `nosniff` + 固定 `octet-stream` | 上传一个 `.html` 再让它内联渲染，等于在自己域上跑别人的脚本 |

用 `http.ServeContent` 而不是自己读文件再写：它按块流式传输（1G 的机器上把整个文件读进内存就爆了），
并且白送 Range 支持。

**一致性顺序**：上传是「先落文件、再写库」，删除是「先删库、再删文件」。
两次取舍方向一致——**宁可留孤儿文件，也不留指向空文件的记录**。
孤儿由 `cleanup:orphan_files` 任务定期回收。

## 5.4 定时任务为什么不能存在库里

若依是「数据库存 cron + 反射调用 bean 方法」，Go 里走不通：没有安全的反射调用任意函数的方式。

所以任务在代码里注册（`internal/job`），数据库只存开关与 cron 表达式。
界面上不提供新建任务——造出来的任务永远不会被执行。

库里还存**执行结果**（`sys_jobs.last_*` 的最近一次 + `sys_job_logs` 的每次一行），
这是两件事：「任务是什么」只能在代码里，而「它跑得怎么样」是数据。

启动时按注册表 upsert（`ON CONFLICT DO NOTHING`，不覆盖用户改过的值），
库里多出来的 key 只告警不拒绝启动。**这与权限码的处理相反**：
一条永远不会跑的任务记录是无害的，而一条无效的权限码会让授权静默失效。

时区：cron 按**服务器本地时区**解析（「每天凌晨 3 点」是运维的直觉），执行时间落库仍是 UTC。

## 5.5 配置为什么是「文件 + 环境变量」两层

优先级 **环境变量 > 配置文件 > 内置默认值**，且**没有配置文件也能跑**。

| 决定 | 理由 |
| --- | --- |
| 保留环境变量且优先级最高 | systemd 与容器只能给环境变量。让它们能覆盖单个值，不必去改（只读挂载的）配置文件 |
| 没有配置文件不是错误 | 「clone 下来直接跑」是开源项目的底线体验。默认值就是一套能跑的单机配置 |
| 配置文件**未知字段直接报错** | `KnownFields(true)`。否则把 `max_mb` 写成 `max_size` 不会有任何提示，用户只会觉得「我明明配了怎么不生效」——而这类问题极难往配置上想 |
| 环境变量**取值非法直接报错** | 同理。静默忽略的话 `APP_UPLOAD_MAX_MB=二十` 会安静地退回默认值 |
| 启动时校验语义组合 | `quota_mb < max_mb` 能跑起来但一个文件都传不上去，不如启动就拒绝 |
| 启动打印一行摘要 | 路径一律绝对路径。「数据在哪」「文件传到哪」是运维最常问的两个，而配置里写的 `data.db`、`uploads` 是相对工作目录的 |

`config.yaml` 进 `.gitignore`（含监听地址、代理地址这类逐部署不同的信息），
提交的样例是 `server/config/config.example.yaml`。

## 5.6 运行状态采集为什么不引第三方库

服务监控读的是 CPU / 内存 / 磁盘 / 进程，全部用标准库直接读 `/proc` 与 `Statfs`，
没有引入 `gopsutil` 这类库。理由：

- 需要的字段只有十来个，`/proc/stat`、`/proc/meminfo`、`/proc/self/statm` 各解析一行就够
- 这类库为了跨平台会带一大堆平台实现，而本项目的部署目标是 Linux

**代价是 macOS 与 Windows 上读不到 CPU 与内存**（没有 `/proc`）。处理方式是每个指标都带
`available` 标记，读不到就返回 `available: false`，让界面明说「本平台读不到该指标」。

这一条是刻意的：**返回 0 会被当成「负载很低」，比没有更糟**。

磁盘是唯一必须按平台分文件的部分：`syscall.Statfs` 只有 Unix 有，
Windows 走 kernel32 的 `GetDiskFreeSpaceExW`（同样是标准库，不加依赖）。
见 `internal/system/disk_unix.go` 与 `disk_windows.go`。

> 平台相关的代码在本机能编译**不代表**在别处也能编译。
> `make check-cross` 会对 5 个平台各编译一遍，专门防这一类错误。

几处实现细节：

| 细节 | 为什么 |
| --- | --- |
| CPU 使用率取两次 `/proc/stat` 的差值（间隔 200ms） | 累计值只能算出「开机至今的平均值」，对排查当前状况没有意义 |
| 内存用 `MemAvailable` 而不是 `MemFree` | `MemFree` 不含可回收的页缓存，照它判断会以为内存快满了 |
| 进程 RSS 读 `/proc/self/statm` 而不是 `syscall.Getrusage` | 后者的 `Maxrss` 在 Linux 是 KB、在 macOS 是字节，同一字段两种单位早晚算错 |
| `/proc/stat` 只取前 8 个字段 | 内核已把 `guest`/`guest_nice` 计进 `user`/`nice`，全加会算两遍，使用率偏高 |
| `/proc/meminfo` 只认带 `kB` 单位的行 | 忽略单位会把 `1000 MB` 当成 `1000 kB`，差 1000 倍 |
| `MemAvailable` 缺失时报「不可用」而不是当作 0 | 当作 0 会算出「100% 已用」，是个假告警 |
| 磁盘使用率按 `total - Bfree` 算，而界面上的「可用」显示 `Bavail` | `Bavail` 才是非 root 用户真正能写进去的量（ext4 默认给 root 留 5%），所以使用率跟 `df` 一致；代价是三个数字相加会小于 `total` |

这些解析逻辑只在 Linux 上跑，而开发机通常是 macOS，所以
`internal/system/system_test.go` 用**真实的 `/proc` 内容**做样本——
光靠「在 Linux 上编译通过」验证不了任何解析对不对。

## 6. 错误键清单

后端定义于 `server/internal/httpx/response.go`，前端字典在 `web/src/lib/i18n/zh-CN.ts`。
**两边必须同步**——新增错误键时改两处。

```
error.notFound              error.tooManyRequests
error.validationFailed      error.serviceUnavailable
error.bodyTooLarge          error.invalidId
error.malformedBody         error.internal
error.hasDependents         error.protected
error.cannotDeleteSelf      error.lastAdmin
error.invalidParent         error.badCredentials
error.accountDisabled       error.unauthorized
error.forbidden             error.csrfInvalid
error.duplicate             error.invalidPermCode
error.cannotKickSelf        error.frontendDisabled
error.invalidJobCron        error.fileTooLarge
error.quotaExceeded         error.invalidFile
error.wrongOldPassword
# 以下两个仅由前端产生（web/src/lib/api/client.ts），后端不会返回：
error.network               # fetch 抛异常：没有任何东西应答
error.backendUnreachable    # 有响应但不是信封：请求被代理拦下，或后端没启动
```

除最后两个外，这些键都定义在 `server/internal/httpx/response.go`，文案在前端字典里。
两侧靠字符串约定耦合，漏登记不会报错，界面只会把 `error.notFound` 这样的原始键名直接
显示给用户，所以有两条用例兵住：

| 用例 | 扫哪里 |
| --- | --- |
| `TestFrontendDictCoversErrorKeys` | 后端 `response.go` 的键常量 |
| `TestFrontendGeneratedErrorKeysAreTranslated` | 前端 `client.ts` 里写死的键字面量 |

两条都需要，不能只留一条：`error.backendUnreachable` 在后端也有一个常量（它是
「强制前端字典登记它」的载体，后端自己从不返回它），因此能被上一条覆盖到；
而 **`error.network` 在后端没有任何常量**，只有下一条能拦住它。

## 7. 1C1G 约束下的设计取舍

| 手段 | 位置 | 作用 |
| --- | --- | --- |
| 按 IP 限流 20 rps / burst 40 | `middleware.RateLimit` | 防突发流量打崩进程；**按 IP 而非全进程**，否则单个客户端刷满配额会把所有用户一起打成 429 |
| 限流器惰性 GC（3 分钟） | 同上 | 不清理的话被扫描时 map 会随访问过的 IP 无上限增长，这比限流本身更危险 |
| 请求体上限 1 MiB | `middleware.BodyLimit` | 一个超大 body 就能打爆 1G 内存 |
| SQLite 连接池上限 4 | `repository.NewDB` | SQLite 单写者，多余连接只增加内存和锁等待 |
| `busy_timeout=5000` | DSN `_pragma` | 写冲突时等待而非立刻 `SQLITE_BUSY` |
| WAL + `synchronous=NORMAL` | 同上 | 读写不互相阻塞，且比 `FULL` 快很多 |
| `GOMEMLIMIT=700MiB` | systemd 环境变量 | 限制 Go 堆软上限，避免被 OOM Killer 干掉 |

## 8. 待办清单

认证、鉴权、权限缓存、动态路由、审计日志都已经落地（本文 §5 与
[architecture-server.md](architecture-server.md) 里有对应的实现说明），这里只列还没做的。

| 项 | 说明 |
| --- | --- |
| 初始数据库可复现 | 目前 `server/data.db` 随仓库分发。它同时是开发时的活数据库，登录一次就会写入会话与日志，所以提交前要跑 `make db-clean`。最终应改为从 `migrations/` + 种子 SQL 重新生成，不再随仓库分发 |
| 字体体积 | Inter 可变字体包含全部子集，`dist` 里 woff2 共 218 KB。若只面向中英文可裁剪为 latin + latin-ext |

前端侧的待办见 [architecture-web.md](architecture-web.md) §9，后端侧见
[../server/README.md](../server/README.md)「尚未实现」，表结构侧见 [schema.md](schema.md) §11。
