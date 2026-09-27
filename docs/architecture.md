# 整体架构

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
  "code": 0,                       // 0 = 成功，1 = 失败
  "msg":  "ok",                    // 成功为 "ok"；失败为 i18n 键（见 §6）
  "data": { },                     // 只有成功时存在
  "errors": [ ]                    // 只有字段级校验失败时存在
}
```

**语义由 HTTP 状态码承载**，`code` 只作成功/失败标志，不重复表达状态。
这是相对若依原版（HTTP 恒 200 + `code:200/500`）的一个有意偏离——保留 HTTP 语义后，
curl、日志、监控、网关都能直接看出错误率，不用解析 body。

| 场景 | HTTP | `msg` |
| --- | --- | --- |
| 成功 | 200 | `ok` |
| 参数校验失败 | 400 | `error.validationFailed` |
| 请求体格式错误 | 400 | `error.malformedBody` |
| 路径参数非法 | 400 | `error.invalidId` |
| 资源不存在 | 404 | `error.notFound` |
| 请求体过大 | 413 | `error.bodyTooLarge` |
| 触发限流 | 429 | `error.tooManyRequests` |
| 服务不可用 | 503 | `error.serviceUnavailable` |
| 服务器内部错误 | 500 | `error.internal` |

### 3.2 字段级校验失败

后端只给「哪个字段、违反哪条规则、规则参数是多少」，**不给文案**：

```json
{
  "code": 1,
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

1. 响应体永远有 `code` 和 `msg`
2. 失败响应的 `msg` 永远是 i18n 键，**不含自然语言**（否则前端无法翻译）
3. `data.list` 在空结果时是 `[]` 而非 `null`（否则前端要特判）
4. 列表接口的分页结构固定为 `{list, total, page, page_size}`
5. 时间字段输出 RFC 3339 UTC（Go `time.Time` 的默认 JSON 序列化）

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
| **HTTP 语义状态码** | 若依式 HTTP 恒 200 | 见 §3.1 |
| **后端只出 i18n 键** | 后端按 `Accept-Language` 输出文案 | 文案只需存在一处（前端，UI 所在处）；切换语言无需重新请求；后端保持语言中立，多语言用户都能用 |
| **前端手写 i18n** | svelte-i18n / paraglide-js | 2 个语种、消息量小，运行时库的动态加载与格式化插件全用不上；手写版靠 TS 类型保证缺键编译报错 |
| **版本化迁移** | `CREATE TABLE IF NOT EXISTS` | 后者无法改表结构，等于"上线后手工改库" |
| **认证用 Cookie + 服务端 session** | 无状态 JWT | 单机单进程，JWT 的水平扩展优势为零，但"无法主动登出/踢人/权限变更不实时"的缺点一个不落。且若依本身的 JWT 也只是个壳，真实状态在 Redis——我们没有 Redis，session 表语义等价（**已定，尚未实现**） |
| **不引入 Redis** | Redis 存 session | 单机上 Redis 只增加一个部署单元和一个故障点。同机 SQLite 主键查询（~1-3 µs）比 Redis over loopback（~30-60 µs）还快 |
| **带内容哈希的静态资源永久缓存** | 统一 `no-cache` | 省掉首屏之外的全部重复请求；哈希保证不会拿错版本 |

## 6. 错误键清单

后端定义于 `server/internal/httpx/response.go`，前端字典在 `web/src/lib/i18n/zh-CN.ts`。
**两边必须同步**——新增错误键时改两处。

```
error.notFound              error.tooManyRequests
error.validationFailed      error.serviceUnavailable
error.bodyTooLarge          error.invalidId
error.malformedBody         error.internal
error.network               # 仅前端使用：fetch 抛异常时
```

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

### 8.1 已确定方案、尚未实现

| 项 | 方案 |
| --- | --- |
| 认证 | Cookie + 服务端 session 表（HttpOnly / SameSite=Lax），配 CSRF 校验 |
| 鉴权 | 若依式 RBAC：用户 / 角色 / 菜单 / API 权限；权限点**以代码为准**，在路由上声明（`perm("system:user:add")`） |
| 权限缓存 | 进程内 `map[userID]permSet` + 全局版本号，改角色/菜单时 bump 版本整体失效 |
| 动态路由 | 后端返回菜单树，前端用 `import.meta.glob` 把 `component` 字段映射到页面模块 |

### 8.2 待处理

| 项 | 说明 |
| --- | --- |
| **LICENSE 文件缺失** | 多个文件头部声明"许可证见 LICENSE 文件"，但仓库没有该文件。要么补上 Apache 2.0 全文，要么去掉声明 |
| 表结构设计 | `sys_user` / `sys_role` / `sys_menu` 等表由项目维护者设计，之后追加 `migrations/0002_*.sql` |
| 初始数据库可复现 | 目前 `server/data.db` 随仓库分发。最终应改为从 `migrations/` + 种子 SQL 重新生成，而不是手工改库后提交 |
| 字体体积 | Inter 可变字体包含全部子集，`dist` 里 woff2 共 224 KB。若只面向中英文可裁剪为 latin + latin-ext |
| 操作日志表 | 若依的 `sys_oper_log` 需要每请求一写。SQLite 是单写者，届时应改成内存 channel 缓冲 + 批量落库，而不是直接写库 |
