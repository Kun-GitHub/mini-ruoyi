# 数据库 Schema

[English](schema.en.md) | 简体中文

本文是数据库结构的**唯一真源**。代码里的实体、迁移、种子数据都以本文为准。

数据库：SQLite（`modernc.org/sqlite`，纯 Go 无 CGO）。不使用 ORM。

## 1. 设计原则

> **字段必须被代码消费，或被界面展示。两者皆无者删。**

配套的三条推论：

1. **不做多租户**——`tenant_id` 全删
2. **不做软删除**——用 `status` 停用替代删除
3. **不做字典表**——枚举的中英文标签由前端 i18n 字典提供（见 §7）

原始设计稿（从 PostgreSQL 导出）有 10 张表 / 89 列，按此原则裁到 **6 张表 / 35 列**。
每一处删除的理由见 §9 决策记录。

## 2. 表清单

| 表 | 类型 | 说明 |
| --- | --- | --- |
| `sys_users` | 实体 | 用户 |
| `sys_roles` | 实体 | 角色 |
| `sys_menus` | 实体（部署期配置） | 菜单树，由迁移种入，不对用户开放"新建" |
| `sys_user_roles` | 关联 | 用户 ↔ 角色 |
| `sys_role_menus` | 关联 | 角色 ↔ 菜单（控制导航可见性） |
| `sys_role_perms` | 授权 | 角色 ↔ 权限码（控制 API 可调用性） |
| `sys_sessions` | 基础设施 | 登录会话。不参与 RBAC，由认证层维护 |
| `sys_login_logs` | 审计 | 登录尝试（成功与失败都记） |
| `sys_oper_logs` | 审计 | 写操作审计 |
| `sys_files` | 工具 | 文件元数据。文件本体在磁盘上 |
| `sys_jobs` | 工具 | 定时任务的**可调参数**。任务本体在代码里 |
| `sys_job_logs` | 审计 | 任务执行历史。每次执行（含被跳过）一条，只追加

## 3. 字段定义

### 3.1 `sys_users`

| 列 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | `INTEGER` | PK AUTOINCREMENT | **必须写 `INTEGER`**，写 `BIGINT` 会失去 rowid 别名（见 §6.3） |
| `created_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | SQLite 无 `ON UPDATE`，每条 UPDATE 语句都要显式 `SET updated_at = CURRENT_TIMESTAMP` |
| `status` | `varchar` | NOT NULL, DEFAULT `'active'`, CHECK IN (`active`,`inactive`) | |
| `username` | `varchar(128)` | NOT NULL, **UNIQUE** | 登录身份，不可变 |
| `password` | `varchar(255)` | NOT NULL | bcrypt 哈希（60 字符，255 余量充足） |
| `nickname` | `varchar(128)` | NOT NULL, DEFAULT `''` | 界面显示名 |
| `mobile` | `varchar(20)` | NOT NULL, DEFAULT `''` | |
| `email` | `varchar(64)` | NOT NULL, DEFAULT `''` | |
| `login_ip` | `varchar(64)` | NOT NULL, DEFAULT `''` | 登录时写入 |
| `login_at` | `datetime` | NULL | NULL 表示从未登录。**不用 `0` 表示"从未"**，那是两种不同的数据 |

不设软删除。用户"删除"即物理删除；要保留访问记录就用 `status = 'inactive'` 停用。
代价是删除不可撤销——这是为换取"每条 SELECT 都不必带 `deleted_at IS NULL`"付的价。
漏写一次那个条件就是静默的数据泄漏，成本远高于收益。

### 3.2 `sys_roles`

| 列 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | `INTEGER` | PK AUTOINCREMENT | |
| `created_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `status` | `varchar` | NOT NULL, DEFAULT `'active'`, CHECK IN (`active`,`inactive`) | |
| `code` | `varchar(64)` | NOT NULL, **UNIQUE** | 稳定标识，如 `admin`。代码据此识别内置角色 |
| `name` | `varchar(128)` | NOT NULL | 显示名 |
| `remark` | `varchar(255)` | NOT NULL, DEFAULT `''` | |

`code` 是必需的：`id` 是自增值，不同环境不同值，代码里没法用它判断"这是超级管理员角色"。

### 3.3 `sys_menus`

菜单是**部署期配置**，不是运行时数据。原因见 §8.1。

| 列 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | `INTEGER` | PK AUTOINCREMENT | |
| `created_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `status` | `varchar` | NOT NULL, DEFAULT `'active'`, CHECK IN (`active`,`inactive`) | ⚠️ 只控制导航可见性，**不控制接口权限**，见 §8.3 |
| `parent_id` | `INTEGER` | NULL, **REFERENCES `sys_menus(id)` ON DELETE CASCADE** | NULL 表示根节点 |
| `sort` | `INTEGER` | NOT NULL, DEFAULT 0 | 同级排序 |
| `menu_type` | `varchar` | NOT NULL, DEFAULT `'directory'`, CHECK IN (`directory`,`menu`) | 只有两态，`button` 型已被权限码取代 |
| `title_key` | `varchar(128)` | NOT NULL | **i18n 键**，如 `menu.system.users`，见 §7 |
| `path` | `varchar(255)` | NOT NULL, DEFAULT `''` | 前端路由路径 |
| `component` | `varchar(255)` | NOT NULL, DEFAULT `''` | 前端页面模块标识；`directory` 型为空 |
| `icon` | `varchar(64)` | NOT NULL, DEFAULT `''` | lucide 图标名 |

**`parent_id` 用 NULL 而不是 `0`**，这一点纠正了我之前的建议。用 `0` 表示根节点
固然是若依的写法，但那样拿不到外键约束：删掉一个目录后，它的子菜单会变成
`parent_id = <已删除的 id>`，在树里永久不可见——静默的数据损坏。
用 NULL + 自引用外键 + `ON DELETE CASCADE`，删父节点会自动带走整棵子树，这才是想要的行为。

### 3.4 `sys_user_roles`

| 列 | 类型 | 约束 |
| --- | --- | --- |
| `user_id` | `INTEGER` | NOT NULL, REFERENCES `sys_users(id)` ON DELETE CASCADE |
| `role_id` | `INTEGER` | NOT NULL, REFERENCES `sys_roles(id)` ON DELETE CASCADE |

复合主键 `(user_id, role_id)` + 反向索引 `role_id`（见 §5）。

### 3.5 `sys_role_menus`

| 列 | 类型 | 约束 |
| --- | --- | --- |
| `role_id` | `INTEGER` | NOT NULL, REFERENCES `sys_roles(id)` ON DELETE CASCADE |
| `menu_id` | `INTEGER` | NOT NULL, REFERENCES `sys_menus(id)` ON DELETE CASCADE |

复合主键 `(role_id, menu_id)` + 反向索引 `menu_id`。

### 3.6 `sys_role_perms`

| 列 | 类型 | 约束 |
| --- | --- | --- |
| `role_id` | `INTEGER` | NOT NULL, REFERENCES `sys_roles(id)` ON DELETE CASCADE |
| `perm_code` | `varchar(64)` | NOT NULL，形如 `system:user:add` |

复合主键 `(role_id, perm_code)` + 反向索引 `perm_code`。

**没有 `sys_apis` 表。** 权限码不引用任何 id，它本身就是与代码共享的字符串常量。
详见 §8。

### 3.7 `sys_sessions`

认证方案是 Cookie + 服务端 session（见 [architecture.md](architecture.md) 的决策记录），
所以会话状态必须落库——这样「登出 / 踢人 / 停用用户」才能立即生效。

| 列 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `token_hash` | `varchar(64)` | PK | cookie 里那个 token 的 SHA-256（base64url）。**不存明文** |
| `user_id` | `INTEGER` | NOT NULL, REFERENCES `sys_users(id)` ON DELETE CASCADE | 删用户即清会话 |
| `created_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `expires_at` | `datetime` | NOT NULL | 固定 **7 天**，不做滑动续期 |
| `last_seen_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | 节流刷新（≥5 分钟才写一次） |
| `csrf_token` | `varchar(64)` | NOT NULL | 随会话下发，前端放请求头回传 |
| `ip` | `varchar(64)` | NOT NULL, DEFAULT `''` | |
| `user_agent` | `varchar(255)` | NOT NULL, DEFAULT `''` | |

**为什么不存明文 token**：`data.db` 随仓库分发，用户也可能随手备份。明文 token
等同于把所有人的会话直接送出去；SHA-256 无法反推。

**为什么 `last_seen_at` 要节流**：SQLite 是单写者。每个请求都写一行会让所有请求在
写锁上排队，而管理后台看不出 5 分钟的精度差异，写量却从「每请求」降到「每会话每 5 分钟」。

**为什么不滑动续期**：滑动续期会让一个长期不关的页面永远不会失效——安全性反而更差。
固定 7 天下即使 cookie 被窃取也会自然到期。7 天意味着每天使用的管理员大约每周重登一次，
对管理后台可以接受；长有效期的风险由 HttpOnly、SameSite=Lax + CSRF 头、
以及服务端可立即删会话（登出 / 停用用户 / 改密码）共同兜住。

索引：`user_id`（踢人）、`expires_at`（清理过期）。

### 3.8 `sys_login_logs` / `sys_oper_logs`

审计表。两条共同的设计约束：

- **没有 `updated_at`**——日志被改过就失去审计价值
- **`user_id` 不设外键**，`username` 也冗余存一份——用户被删除后日志必须留下，
  否则删号就能抹掉痕迹

| `sys_login_logs` | 说明 |
| --- | --- |
| `username` / `status` / `reason` | `status` 是 `success`/`failed`，`reason` 是失败原因的 i18n 键 |
| `ip` / `user_agent` | 排查靠这两个 |

| `sys_oper_logs` | 说明 |
| --- | --- |
| `user_id` / `username` | 操作者 |
| `method` / `path` | 动了哪个接口 |
| `status` / `result` | HTTP 状态码；失败时 `result` 是 i18n 键 |
| `duration_ms` | 耗时 |
| `ip` / `user_agent` | 来源 |

**不记录请求体**：登录、改密码、重置密码这些接口的 body 里是明文密码，
而日志表的访问控制比用户表弱得多，写进去等于自己制造一个泄密点。
`method + path` 已经能回答「谁在什么时候动了哪个资源」。

**记录范围**：非 GET/HEAD/OPTIONS 的请求，外加任何以 401/403 结束的请求。
后者是安全审计最关心的——「谁在反复试探自己没有权限的接口」。

**写入方式见 [architecture.md](architecture.md) §5.2**：批量落库，不是每请求直写。

索引：`created_at`（保留期清理）、`username`/`user_id` + `created_at`（按人查历史）。

### 3.9 `sys_files`

元数据进库，**文件本体落磁盘**（`APP_UPLOAD_DIR`）。

⚠️ **不能存 BLOB**：`data.db` 是随仓库分发的，BLOB 会让仓库无限膨胀，git 也没法管理二进制。

| 列 | 说明 |
| --- | --- |
| `group_name` | 分组。**不做目录树**——树要处理移动/重命名/级联删除，而后台的文件本质是「上传的附件」 |
| `original_name` | 用户给的文件名，只用于展示与下载时的文件名 |
| `storage_path` | 磁盘相对路径（`<前两位>/<32位随机hex>`），**绝不使用用户输入** |
| `uploader_id` / `uploader_name` | 上传者。用户名冗余一份，用户被删后记录仍可读 |

`storage_path` 用随机名而不是自增 id：自增名可以被顺序枚举，而文件名不该是个可猜的 URL。
分两级目录：上万文件挤一个目录在某些文件系统上会明显退化。

### 3.10 `sys_jobs`

**任务本体在代码里**（`server/internal/job`），这张表只存可调参数。

| 列 | 说明 |
| --- | --- |
| `job_key` | 稳定标识，与代码注册表一一对应 |
| `cron` | 标准 5 段表达式（分 时 日 月 周），按**服务器本地时区**执行 |
| `status` | `active` / `inactive` |
| `last_run_at` / `last_status` / `last_error` / `last_duration_ms` | 最近一次执行结果。列表页的快路径，不用聚合子表 |

`last_status` 有 `skipped` 一态：表示这次因为「上一次还没跑完」被跳过。
单独一个状态是有必要的——它解释「为什么今天没跑」。

**执行历史另建一表**（`sys_job_logs`，见 §3.11）：这四个字段只留最近一次，
而「这个任务最近十次跑了多久」「昨晚那次为什么失败」答不上来。
保留期不另设配置，共用 `sys_login_logs` / `sys_oper_logs` 的
`APP_LOG_RETENTION_DAYS`（默认 30 天）——「日志保留多久」是一个问题，
不该有两个答案。

### 3.11 `sys_job_logs`

任务执行历史。追加写，每次执行一行。

| 列 | 说明 |
| --- | --- |
| `job_key` | 与代码注册表对应。**不设外键**——任务从代码里删掉后，它跑过的记录必须留下 |
| `trigger` | `cron`（调度器触发）/ `manual`（界面上点了「立即执行」）。「这个任务怎么跑了两遍」的答案通常就在这一列 |
| `status` | `success` / `failed` / `skipped`，与 `sys_jobs.last_status` 取值一致，但**没有空串**——只有真的跑过（或被跳过）才会写这行 |
| `error` | 失败或跳过原因，成功时为空 |
| `duration_ms` | 耗时 |

与其余审计表同规矩：**没有 `updated_at`**，只追加不修改。
`created_at` 用执行开始时间，与 `sys_jobs.last_run_at` 是同一个时刻。

写入方式：追加日志行与更新 `sys_jobs.last_*` 在**同一个事务**里完成。
分开写的话，进程恰好停在两次写中间，就会留下「列表说上次成功了、
历史里却查不到」这种自相矛盾的两份现实。

索引：`(job_key, id)`（按任务翻历史）、`created_at`（保留期清理）。

### 3.12 为什么任务不能像若依那样存在库里

若依是「数据库存 cron + 反射调用 bean 方法」。**Go 里这条路走不通**：
没有安全的反射调用任意函数的方式。

所以任务只能代码注册，界面上**不提供新建任务**：造出来的任务永远不会被执行，
因为没有任何代码会跑它。这与权限点不能在界面上新建是同一个道理。

启动时按代码注册表 **upsert**（`ON CONFLICT DO NOTHING`，不覆盖用户改过的 cron），
库里多出来的 key 只**告警**不拒绝启动——与权限码的处理相反：
一条永远不会跑的任务记录是无害的，而一条无效的权限码会让授权静默失效。

## 4. DDL

```sql
CREATE TABLE sys_users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     varchar  NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    username   varchar(128) NOT NULL UNIQUE,
    password   varchar(255) NOT NULL,
    nickname   varchar(128) NOT NULL DEFAULT '',
    mobile     varchar(20)  NOT NULL DEFAULT '',
    email      varchar(64)  NOT NULL DEFAULT '',
    login_ip   varchar(64)  NOT NULL DEFAULT '',
    login_at   datetime     NULL
);

CREATE TABLE sys_roles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     varchar  NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    code       varchar(64)  NOT NULL UNIQUE,
    name       varchar(128) NOT NULL,
    remark     varchar(255) NOT NULL DEFAULT ''
);

CREATE TABLE sys_menus (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     varchar  NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    parent_id  INTEGER  NULL REFERENCES sys_menus(id) ON DELETE CASCADE,
    sort       INTEGER  NOT NULL DEFAULT 0,
    menu_type  varchar  NOT NULL DEFAULT 'directory'
                        CHECK (menu_type IN ('directory','menu')),
    title_key  varchar(128) NOT NULL,
    path       varchar(255) NOT NULL DEFAULT '',
    component  varchar(255) NOT NULL DEFAULT '',
    icon       varchar(64)  NOT NULL DEFAULT ''
);

CREATE TABLE sys_user_roles (
    user_id INTEGER NOT NULL REFERENCES sys_users(id) ON DELETE CASCADE,
    role_id INTEGER NOT NULL REFERENCES sys_roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE sys_role_menus (
    role_id INTEGER NOT NULL REFERENCES sys_roles(id) ON DELETE CASCADE,
    menu_id INTEGER NOT NULL REFERENCES sys_menus(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, menu_id)
);

CREATE TABLE sys_role_perms (
    role_id   INTEGER NOT NULL REFERENCES sys_roles(id) ON DELETE CASCADE,
    perm_code varchar(64) NOT NULL,
    PRIMARY KEY (role_id, perm_code)
);

CREATE TABLE sys_files (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at    datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    group_name    varchar(64)  NOT NULL DEFAULT '',
    original_name varchar(255) NOT NULL,
    storage_path  varchar(255) NOT NULL UNIQUE,
    size          INTEGER      NOT NULL,
    content_type  varchar(128) NOT NULL DEFAULT '',
    uploader_id   INTEGER      NOT NULL DEFAULT 0,
    uploader_name varchar(128) NOT NULL DEFAULT ''
);

CREATE TABLE sys_jobs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     varchar NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    job_key    varchar(64) NOT NULL UNIQUE,
    cron       varchar(64) NOT NULL,
    remark     varchar(255) NOT NULL DEFAULT '',
    last_run_at      datetime NULL,
    last_status      varchar(16) NOT NULL DEFAULT ''
                     CHECK (last_status IN ('', 'success', 'failed', 'skipped')),
    last_error       varchar(255) NOT NULL DEFAULT '',
    last_duration_ms INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE sys_job_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    job_key    varchar(64) NOT NULL,
    trigger    varchar(16) NOT NULL CHECK (trigger IN ('cron', 'manual')),
    status     varchar(16) NOT NULL CHECK (status IN ('success', 'failed', 'skipped')),
    error      varchar(255) NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_job_logs_key     ON sys_job_logs (job_key, id);
CREATE INDEX idx_job_logs_created ON sys_job_logs (created_at);

CREATE TABLE sys_login_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    username   varchar(128) NOT NULL,
    status     varchar(16)  NOT NULL CHECK (status IN ('success','failed')),
    reason     varchar(64)  NOT NULL DEFAULT '',
    ip         varchar(64)  NOT NULL DEFAULT '',
    user_agent varchar(255) NOT NULL DEFAULT ''
);

CREATE TABLE sys_oper_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at  datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_id     INTEGER      NOT NULL DEFAULT 0,
    username    varchar(128) NOT NULL DEFAULT '',
    method      varchar(10)  NOT NULL,
    path        varchar(255) NOT NULL,
    status      INTEGER      NOT NULL,
    result      varchar(64)  NOT NULL DEFAULT '',
    duration_ms INTEGER      NOT NULL DEFAULT 0,
    ip          varchar(64)  NOT NULL DEFAULT '',
    user_agent  varchar(255) NOT NULL DEFAULT ''
);

CREATE TABLE sys_sessions (
    token_hash   varchar(64) PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES sys_users(id) ON DELETE CASCADE,
    created_at   datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   datetime NOT NULL,
    last_seen_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    csrf_token   varchar(64) NOT NULL,
    ip           varchar(64)  NOT NULL DEFAULT '',
    user_agent   varchar(255) NOT NULL DEFAULT ''
);
```

## 5. 索引

| 索引 | 理由 |
| --- | --- |
| `sys_users.username` | UNIQUE，登录查询 |
| `sys_roles.code` | UNIQUE，代码识别内置角色 |
| `sys_menus(parent_id, sort)` | 建树与同级排序 |
| `sys_user_roles(role_id)` | 查"这个角色下有哪些人"。复合主键 `(user_id, role_id)` 覆盖不了纯 `role_id` 查询 |
| `sys_role_menus(menu_id)` | 同上 |
| `sys_role_perms(perm_code)` | 查"哪些角色拥有这个权限" |

复合主键的最左前缀已经覆盖了正向查询，所以只需补反向索引。

## 6. SQLite 特有注意事项（均为实测结论）

### 6.1 `timestamptz` 不可用，必须用 `datetime`

`timestamptz` 是 PostgreSQL 类型。实测各候选声明类型能否扫进 Go 的 `time.Time`：

| 声明类型 | 存储类 | 扫进 `time.Time` |
| --- | --- | --- |
| `timestamptz` | text | ❌ **无法扫描** |
| `timestamp` | text | ✅ |
| `datetime` | text | ✅ |
| `date` | text | ✅ |
| `time` | text | ❌ |
| `varchar` | text | ❌ |

依据是驱动源码 `modernc.org/sqlite/sqlite.go`：

```go
switch r.ColumnTypeDatabaseTypeName(i) {   // = strings.ToUpper(声明类型)
case "DATE", "DATETIME", "TIMESTAMP":      // 精确匹配
    dest[i], _ = r.c.parseTime(v)
default:
    dest[i] = v                            // 拿到的是 string
}
```

是**精确匹配**，所以 `timestamp(3)`、`timestamp with time zone` 同样不行。
本项目统一用 `datetime`。

### 6.2 `varchar(n)` 的长度不被强制

实测：往 `varchar(10)` 写入 500 字符**成功**，实际存了 500 字符。

SQLite 忽略长度约束，`varchar(128)` 里的 128 只是文档。**真正的长度校验必须写在 Go 的
`binding` tag 上**：

```go
Username string `json:"username" binding:"required,min=2,max=128"`
```

两处都要写，但要知道只有后者生效。

### 6.3 `INTEGER PRIMARY KEY` 与 `BIGINT PRIMARY KEY` 不等价

实测：

```
INTEGER  ✅ id 是 rowid 别名（rowid=9007199254740993）
BIGINT   ⚠️ id 走独立主键索引（rowid=1, id=9007199254740993）
```

`BIGINT PRIMARY KEY` 时 SQLite 会额外建一棵主键索引 B-tree，按主键查要两次 B-tree 查找。
所以主键**必须**声明为 `INTEGER PRIMARY KEY`。

### 6.4 `NULL` 与 `0` 是两种不同的数据

实测 `parent_id bigint NULL DEFAULT 0` 时：

```
INSERT DEFAULT VALUES          → parent_id = 0     (typeof=integer)
INSERT (parent_id) VALUES(NULL)→ parent_id = NULL  (typeof=null)
```

所以"既可空又有默认值 0"是自相矛盾的，查询"根节点"时必须同时处理两者。
本 schema 的做法是**只允许一种**：`parent_id` 用 NULL（见 §3.3），
`login_at` 用 NULL，其余字符串字段用 `NOT NULL DEFAULT ''`。

### 6.5 其他

- `boolean` 声明类型可以正常扫进 Go 的 `bool`，`true`/`false` 字面量存成整数 1/0
- `CHECK` 约束在 SQLite 里**是真实生效的**，所以枚举值用它兜住
- SQLite 没有 `ON UPDATE CURRENT_TIMESTAMP`，每条 UPDATE 语句都必须显式
  `SET updated_at = CURRENT_TIMESTAMP`（写在 SQL 里，不用 Go 算时间，避免应用与库的时钟差异）

## 7. 与前端 i18n 的约定

菜单标题存的是 **i18n 键**（`title_key`），不是文案：

```
sys_menus.title_key = 'menu.system.users'
  → zh-CN: '用户管理'
  → en-US: 'Users'
```

这与既有架构一致：**后端只出键，文案归前端**（见 [architecture.md](architecture.md)）。

之所以敢这么做，是因为菜单必须对应前端的页面组件——**新建一个菜单必然要同时写一个前端页面**，
所以不存在"用户在界面上建了菜单却没有文案"的场景。菜单本质是部署期配置（见 §8.1）。

新增菜单的完整动作：写迁移种入菜单行 + 在 `web/src/lib/i18n/zh-CN.ts` 与 `en-US.ts` 加 `menu.*` 键。

枚举值的标签（`status`、`menu_type`）同样走前端 i18n 字典，这是删掉字典表的原因。
具体见 [architecture-web.md](architecture-web.md)。

## 8. 权限模型

已定方案：**代码声明 + 启动校验**。

### 8.1 权限码是代码常量

权限码形如 `system:user:add`，声明在路由注册处：

```go
users.POST("", perm(perm.SystemUserAdd), h.Create)
```

`perm.SystemUserAdd = "system:user:add"` 定义在 Go 代码里，是唯一真源。

权限清单 UI 通过 `GET /api/v1/perms` 获取——该接口由代码里的路由表生成，
按路径前缀分组，**不查数据库**。

### 8.2 内置 `admin` 角色隐式放行

`sys_roles.code = 'admin'` 的角色**不写** `sys_role_perms` 和 `sys_role_menus`，
由代码直接判走：

```go
func (s *AuthzService) allowed(u *domain.User, code string) bool {
    if u.HasRole("admin") { return true }   // 隐式放行
    return s.permCache.Has(u.ID, code)
}
```

这么做的理由有两个：

1. **避免种子顺序问题。** 若 admin 的权限要落到 `sys_role_perms`，那这批权限码
   必须与代码里声明的集合完全一致——但初始化时端点还不存在，写进去就会被
   §8.3 的启动校验判为“多余的权限码”而拒绝启动
2. **避开一类常见的运维事故。** 显式授权时，只要有人在界面上误改了超级管理员角色的
   权限，就可能把自己锁在系统外，而修复需要手工改库

代价是超级管理员角色**不可被限制**。如果你需要“受限的管理员”，另建一个普通角色即可。

### 8.3 ⚠️ 菜单可见性与接口权限是两回事

菜单和权限码是**解耦**的。所以把一个菜单设成 `status = 'inactive'`：

- ✅ 侧边栏不显示
- ❌ **接口仍然可以被调用**，只要角色持有对应 `perm_code`

「隐藏菜单」不等于「禁用功能」。要真正禁用，必须撤销角色的权限码。

### 8.4 权限点为什么不能在界面上管理

界面上有「API 权限」页面（`/system/apis`），但它**只读**。这不是偷懒：

一个权限点要真正生效，必须同时存在三样东西：

1. **代码里的声明**（`internal/perm`）——决定它出现在清单里
2. **某个路由对它的引用**（`registrar.protect(..., code, ...)`）——决定它守护哪个接口
3. **请求时的校验**（`middleware.RequirePerm`）——决定勾了之后真的会拦

只往数据库写一行权限码，三样里一样都没有：它不会守护任何接口，
勾了也不生效。那是「看着像权限、其实是装饰」的记录，比没有更糟。

所以这个页面提供的是**查看**（权限码 ↔ 方法 + 路径），授权仍然在角色那边。
`GET /perms` 返回的 `endpoints` 直接来自启动时装配好的路由表，不查库——
它和实际的接口不可能不一致。

`TestEveryDeclaredPermIsUsedByARoute` 反向堵住另一半：声明了却没有路由引用的
权限码会出现在授权界面里却不起作用，必须让用例失败。

### 8.5 启动校验

服务启动时比对两处：

1. 代码里声明的全部权限码（集合 A）
2. `sys_role_perms` 表里出现的全部 `perm_code`（集合 B）

**`B - A` 非空则启动失败**，并列出多出来的权限码。

这挡住的是最危险的一类问题：权限码改名后，DB 里的旧授权记录既不会报错也不会生效，
只会静默地少给权限（或更糟，权限码被复用后静默多给权限）。

## 9. 决策记录

### 9.1 整表删除

| 表 | 理由 |
| --- | --- |
| `sys_depts` | **零消费方**。原设计的 `sys_roles` 没有 `data_scope` 字段，部门不参与数据权限；其余任何表或功能都不读取部门。若将来需要，加表比改表安全 |
| `sys_dicts` + `sys_dict_datas` | 前端 i18n 字典已覆盖枚举标签这个主要用途。DB 驱动的字典要额外付出：2 个 CRUD 模块、缓存失效逻辑、"字典改了但前端没刷新"这一类问题 |
| `sys_apis` | 权限码改成代码常量后，`(api_path, api_method)` 是代码派生的冗余数据。保留它还要多付：一个 id 空间、开机 upsert 同步、路径参数模式匹配、以及"DB 与代码不一致时静默失效"的风险 |

### 9.2 整类字段删除

| 字段 | 出现次数 | 理由 |
| --- | --- | --- |
| `created_by` `updated_by` | 14 列 | 无任何消费方（界面不展示、代码不读取）；`NOT NULL` 逼种子数据写假值（初始化时没有登录用户）；要把当前用户从 handler 透传到 repository，每个仓储方法都要多一个参数 |
| `tenant_id` | 2 列 | 不做多租户 |
| `deleted_at` `deleted_by` | 2 列 | 用 `status` 停用替代。软删除的隐性成本：每条 SELECT 都要带 `IS NULL`（漏一条即静默数据泄漏）、唯一索引要改写成部分索引 |

### 9.3 单列删除

| 表.字段 | 理由 |
| --- | --- |
| `sys_users.position` | 没有岗位体系，纯装饰 |
| `sys_users.dept_id` | 随 `sys_depts` 一起删 |
| `sys_menus.is_redirect` `redirect` | 目录只有一个子项时前端自动跳转，不需要后端存两个字段 |
| `sys_menus.remark` | 菜单名（`title_key`）已把信息说清 |
| `sys_apis.*` | 随表删 |
| `sys_dicts.remark` `sys_dict_datas.remark` | 随表删 |

### 9.4 字段修改

| 原 | 改为 | 理由 |
| --- | --- | --- |
| `id bigint` | `id INTEGER PRIMARY KEY AUTOINCREMENT` | 雪花 ID 是 64 位（~1.8e18），超过 JS `Number.MAX_SAFE_INTEGER`（9007199254740991），前端 `JSON.parse` 会**静默截断**，回传后端必然 404。且单机 SQLite 下雪花本来就没有收益 |
| `timestamptz` | `datetime` | 实测无法扫进 `time.Time`（§6.1） |
| `status` 三态 | 两态 | `suspended` 在整个 schema 里没有任何产生方（没有登录失败计数、没有锁定机制） |
| `menu_type` 三态（含 button） | 两态 | `button` 型的功能已被权限码取代 |
| `sys_menus.name` | `sys_menus.title_key` | 菜单必须对应前端页面，本质是部署期配置；存字符串会让菜单只能是单语言，与已建的双语前端冲突 |
| `sys_role_apis` | `sys_role_perms` | 不再引用 `sys_apis.id`，直接存权限码 |

### 9.5 字段新增

| 字段 | 理由 |
| --- | --- |
| `sys_roles.code` | 代码要识别内置管理员角色，而 `id` 是自增值，不同环境不同值 |
| `sys_roles.remark` | 原设计没有。角色数量少且长期存在，需要一个地方记录“为什么会有这个角色” |
| `sys_menus.sort` | 菜单顺序是用户直接能感知的，靠自增 id 排序是碰运气 |

## 10. 种子数据

由 `migrations/0003_seed_rbac.sql` 种入：

| 内容 | 说明 |
| --- | --- |
| 内置角色 `code = 'admin'` | 超级管理员，隐式放行全部权限（见 §8.2） |
| `admin` 用户 | 密码 `admin123` 的 bcrypt 哈希，**直接写死在迁移里** |
| `admin` 用户的 `sys_user_roles` 关联 | |
| 菜单树 | 三个目录（系统管理 / 系统监控 / 工具）+ 10 个页面菜单，分五次种入 |

**不写 `sys_role_perms` / `sys_role_menus`** —— 见 §8.2。

菜单分批种入是为了照顾已有库：新增菜单时不动旧迁移，追加一条新迁移。
目录用 `WHERE NOT EXISTS` 包一层，所以在旧库上重跑不会出现第二份，
页面菜单则直接以目录为父插入。

| 迁移 | 菜单 |
| --- | --- |
| `0003_seed_rbac.sql` | 系统管理：用户管理 / 角色管理 / 菜单管理 |
| `0005_seed_api_menu.sql` | 系统管理：权限清单（只读页，理由见 §8.4） |
| `0007_seed_monitor_menu.sql` | 系统监控（新目录）：在线会话 / 登录日志 / 操作日志 |
| `0009_seed_tool_menu.sql` | 工具（新目录）：文件管理 / 定时任务 |
| `0011_seed_system_monitor_menu.sql` | 系统监控：服务监控（插到最前，其余三个 `sort` 依次后移） |

### 10.1 初始密码

采用**迁移里写死弱密码** `admin123`。取舍：

- 开源用户 clone 下来即可登录，不需要任何额外步骤
- 代价是弱密码，且不能指望所有用户都会去改

**没有做「首次登录强制改密」**。若要加，需要新增字段（如 `must_change_password`）
和一套拦截逻辑，属于额外功能。

迁移里的 bcrypt 哈希是**手抄**的，抄错一位就会导致谁也登不进去。因此
`migrate_test.go` 的 `TestSeedRbac` 用 `bcrypt.CompareHashAndPassword` 断言它确实
对应 `admin123`。这个用例不是为了覆盖率，是为了兜住抄写错误——实测它确实拦下过
一次漏字符。

## 11. 尚未定义

| 项 | 说明 |
| --- | --- |
| 登录失败锁定 | 若要做，`status` 可能需要加回 `suspended` 或新增 `locked_until` 字段 |
| 数据权限（按部门/按人可见范围） | 当前不做。若要做，"部门"需要重新引入，且 `sys_roles` 要加 `data_scope` |

会话表（§3.7）与日志表（§3.8）曾经也在这个表里，现在已经落地，所以不再列出。
