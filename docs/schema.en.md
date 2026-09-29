# Database schema

English | [简体中文](schema.md)

This document is the **single source of truth** for the database structure. The entities, migrations and seed data in
the code all follow it.

Database: SQLite (`modernc.org/sqlite`, pure Go, no CGO). No ORM.

## 1. Design principles

> **A field must be consumed by code or displayed in the UI. Anything that is neither gets deleted.**

Three corollaries:

1. **No multi-tenancy** — every `tenant_id` is gone
2. **No soft deletes** — use `status` to deactivate instead of deleting
3. **No dictionary tables** — the Chinese and English labels for enums come from the frontend i18n dictionary (see §7)

The original design (exported from PostgreSQL) had 10 tables and 89 columns; by that principle it was cut down to
**6 tables and 35 columns**. The reason for every deletion is recorded in §9.

## 2. Table list

| Table | Kind | Notes |
| --- | --- | --- |
| `sys_users` | entity | users |
| `sys_roles` | entity | roles |
| `sys_menus` | entity (deployment-time configuration) | the menu tree, seeded by migrations; users cannot "create" entries |
| `sys_user_roles` | relation | user ↔ role |
| `sys_role_menus` | relation | role ↔ menu (controls navigation visibility) |
| `sys_role_perms` | grant | role ↔ permission code (controls which APIs are callable) |
| `sys_sessions` | infrastructure | login sessions. Not part of RBAC; maintained by the auth layer |
| `sys_login_logs` | audit | login attempts (successes and failures alike) |
| `sys_oper_logs` | audit | write-operation auditing |
| `sys_files` | tool | file metadata. The file itself lives on disk |
| `sys_jobs` | tool | the **tunable parameters** of scheduled jobs. The jobs themselves live in code |
| `sys_job_logs` | audit | job execution history. One row per run (skipped ones included), append-only |

## 3. Field definitions

### 3.1 `sys_users`

| Column | Type | Constraints | Notes |
| --- | --- | --- | --- |
| `id` | `INTEGER` | PK AUTOINCREMENT | **Must be written `INTEGER`**; `BIGINT` loses the rowid alias (see §6.3) |
| `created_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | SQLite has no `ON UPDATE`, so every UPDATE statement has to set `SET updated_at = CURRENT_TIMESTAMP` explicitly |
| `status` | `varchar` | NOT NULL, DEFAULT `'active'`, CHECK IN (`active`,`inactive`) | |
| `username` | `varchar(128)` | NOT NULL, **UNIQUE** | the login identity, immutable |
| `password` | `varchar(255)` | NOT NULL | bcrypt hash (60 characters; 255 leaves plenty of room) |
| `nickname` | `varchar(128)` | NOT NULL, DEFAULT `''` | the display name |
| `mobile` | `varchar(20)` | NOT NULL, DEFAULT `''` | |
| `email` | `varchar(64)` | NOT NULL, DEFAULT `''` | |
| `login_ip` | `varchar(64)` | NOT NULL, DEFAULT `''` | written at login |
| `login_at` | `datetime` | NULL | NULL means never logged in. **`0` is not used for "never"**, because those are two different kinds of data |

No soft delete. "Deleting" a user is a physical delete; to keep the access history, deactivate with
`status = 'inactive'`.
The price is that deletion is irreversible — that is what buys "no SELECT ever has to carry
`deleted_at IS NULL`". Omitting that condition once is a silent data leak, which costs far more than the benefit.

### 3.2 `sys_roles`

| Column | Type | Constraints | Notes |
| --- | --- | --- | --- |
| `id` | `INTEGER` | PK AUTOINCREMENT | |
| `created_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `status` | `varchar` | NOT NULL, DEFAULT `'active'`, CHECK IN (`active`,`inactive`) | |
| `code` | `varchar(64)` | NOT NULL, **UNIQUE** | a stable identifier such as `admin`. Code uses it to recognize built-in roles |
| `name` | `varchar(128)` | NOT NULL | the display name |
| `remark` | `varchar(255)` | NOT NULL, DEFAULT `''` | |

`code` is required: `id` is an auto-increment value that differs between environments, so code cannot use it to decide
"this is the super-administrator role".

### 3.3 `sys_menus`

Menus are **deployment-time configuration**, not runtime data. The reason is in §8.1.

| Column | Type | Constraints | Notes |
| --- | --- | --- | --- |
| `id` | `INTEGER` | PK AUTOINCREMENT | |
| `created_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `status` | `varchar` | NOT NULL, DEFAULT `'active'`, CHECK IN (`active`,`inactive`) | ⚠️ controls navigation visibility only, **not API permissions**; see §8.3 |
| `parent_id` | `INTEGER` | NULL, **REFERENCES `sys_menus(id)` ON DELETE CASCADE** | NULL means a root node |
| `sort` | `INTEGER` | NOT NULL, DEFAULT 0 | ordering among siblings |
| `menu_type` | `varchar` | NOT NULL, DEFAULT `'directory'`, CHECK IN (`directory`,`menu`) | only two states; the `button` type has been replaced by permission codes |
| `title_key` | `varchar(128)` | NOT NULL | an **i18n key** such as `menu.system.users`; see §7 |
| `path` | `varchar(255)` | NOT NULL, DEFAULT `''` | the frontend route path |
| `component` | `varchar(255)` | NOT NULL, DEFAULT `''` | the frontend page module identifier; empty for `directory` |
| `icon` | `varchar(64)` | NOT NULL, DEFAULT `''` | a lucide icon name |

**`parent_id` uses NULL rather than `0`**, which corrects an earlier suggestion of mine. Using `0` for a root node is
how RuoYi does it, but then you get no foreign key constraint: deleting a directory leaves its child menus with
`parent_id = <the deleted id>` and they become permanently invisible in the tree — silent data corruption.
With NULL, a self-referencing foreign key and `ON DELETE CASCADE`, deleting a parent takes the whole subtree with it,
which is the behaviour we want.

### 3.4 `sys_user_roles`

| Column | Type | Constraints |
| --- | --- | --- |
| `user_id` | `INTEGER` | NOT NULL, REFERENCES `sys_users(id)` ON DELETE CASCADE |
| `role_id` | `INTEGER` | NOT NULL, REFERENCES `sys_roles(id)` ON DELETE CASCADE |

Composite primary key `(user_id, role_id)` plus a reverse index on `role_id` (see §5).

### 3.5 `sys_role_menus`

| Column | Type | Constraints |
| --- | --- | --- |
| `role_id` | `INTEGER` | NOT NULL, REFERENCES `sys_roles(id)` ON DELETE CASCADE |
| `menu_id` | `INTEGER` | NOT NULL, REFERENCES `sys_menus(id)` ON DELETE CASCADE |

Composite primary key `(role_id, menu_id)` plus a reverse index on `menu_id`.

### 3.6 `sys_role_perms`

| Column | Type | Constraints |
| --- | --- | --- |
| `role_id` | `INTEGER` | NOT NULL, REFERENCES `sys_roles(id)` ON DELETE CASCADE |
| `perm_code` | `varchar(64)` | NOT NULL, of the form `system:user:add` |

Composite primary key `(role_id, perm_code)` plus a reverse index on `perm_code`.

**There is no `sys_apis` table.** A permission code references no id; it is itself a string constant shared with the
code. See §8.

### 3.7 `sys_sessions`

The authentication scheme is a cookie plus a server-side session (see the decision record in
[architecture.en.md](architecture.en.md)), so session state has to be persisted — that is what makes "log out / kick a
session / disable a user" take effect immediately.

| Column | Type | Constraints | Notes |
| --- | --- | --- | --- |
| `token_hash` | `varchar(64)` | PK | the SHA-256 (base64url) of the token in the cookie. **The plaintext is not stored** |
| `user_id` | `INTEGER` | NOT NULL, REFERENCES `sys_users(id)` ON DELETE CASCADE | deleting a user clears their sessions |
| `created_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | |
| `expires_at` | `datetime` | NOT NULL | a fixed **7 days**; no sliding renewal |
| `last_seen_at` | `datetime` | NOT NULL, DEFAULT CURRENT_TIMESTAMP | refreshed with throttling (written at most once every 5 minutes) |
| `csrf_token` | `varchar(64)` | NOT NULL | issued with the session; the frontend sends it back in a header |
| `ip` | `varchar(64)` | NOT NULL, DEFAULT `''` | |
| `user_agent` | `varchar(255)` | NOT NULL, DEFAULT `''` | |

**Why the plaintext token is not stored**: `data.db` ships in the repository and users may copy it around, so a
plaintext token is equivalent to handing out everyone's session; SHA-256 cannot be reversed.

**Why `last_seen_at` is throttled**: SQLite is a single writer. Writing a row on every request queues every request on
the write lock, while an admin panel cannot tell the difference at 5-minute granularity — and the write volume drops
from "per request" to "per session per 5 minutes".

**Why there is no sliding renewal**: sliding renewal means a page left open for a long time never expires — that is
worse security, not better. A fixed 7 days means even a stolen cookie expires naturally. Seven days means an
administrator who uses the panel daily logs in roughly once a week, which is acceptable for an admin panel; the risk of
a long validity is covered by HttpOnly, SameSite=Lax plus a CSRF header, and the fact that the server can delete a
session immediately (logout / disabling a user / changing a password).

Indexes: `user_id` (kicking), `expires_at` (expiry cleanup).

### 3.8 `sys_login_logs` / `sys_oper_logs`

Audit tables. Two shared design constraints:

- **No `updated_at`** — a log that has been modified has lost its audit value
- **`user_id` carries no foreign key**, and `username` is stored redundantly — logs have to survive a user being
  deleted, or deleting an account would erase the trail

| `sys_login_logs` | Notes |
| --- | --- |
| `username` / `status` / `reason` | `status` is `success`/`failed`; `reason` is the i18n key for the failure |
| `ip` / `user_agent` | these two are what you investigate with |

| `sys_oper_logs` | Notes |
| --- | --- |
| `user_id` / `username` | who acted |
| `method` / `path` | which endpoint was touched |
| `status` / `result` | the HTTP status code; on failure `result` is an i18n key |
| `duration_ms` | how long it took |
| `ip` / `user_agent` | where it came from |

**Request bodies are not recorded**: the bodies of login, change-password and reset-password carry plaintext
passwords, and the log table's access control is far weaker than the user table's — writing them in means manufacturing
a leak. `method + path` already answers "who touched which resource, and when".

**What gets recorded**: any non-GET/HEAD/OPTIONS request, plus any request that ends in 401/403. The latter is what
security auditing cares about most — "who keeps probing endpoints they have no permission for".

**How they are written is in [architecture.en.md](architecture.en.md) §5.2**: batched, not one write per request.

Indexes: `created_at` (retention cleanup), `username`/`user_id` + `created_at` (history by person).

### 3.9 `sys_files`

Metadata goes in the database and **the file itself goes to disk** (`APP_UPLOAD_DIR`).

⚠️ **A BLOB must not be stored**: `data.db` ships in the repository, so a BLOB would inflate the repository without
limit, and git cannot manage binaries anyway.

| Column | Notes |
| --- | --- |
| `group_name` | grouping. **No directory tree** — a tree means handling moves, renames and cascading deletes, whereas files in an admin panel are essentially "uploaded attachments" |
| `original_name` | the name the user gave, used only for display and as the download filename |
| `storage_path` | the relative path on disk (`<first two characters>/<32 random hex characters>`), **never user input** |
| `uploader_id` / `uploader_name` | the uploader. The username is stored redundantly so the record stays readable after the user is deleted |

`storage_path` uses a random name rather than an auto-increment id: an incrementing name can be enumerated in order, and
a filename should not be a guessable URL. It is two directory levels deep because tens of thousands of files in one
directory degrades noticeably on some filesystems.

### 3.10 `sys_jobs`

**The job itself lives in code** (`server/internal/job`); this table stores only the tunable parameters.

| Column | Notes |
| --- | --- |
| `job_key` | a stable identifier matching the code registry one-to-one |
| `cron` | a standard 5-field expression (minute hour day month weekday), executed in the **server's local time zone** |
| `status` | `active` / `inactive` |
| `last_run_at` / `last_status` / `last_error` / `last_duration_ms` | the result of the most recent run. The fast path for the list page, with no aggregate query against a child table |

`last_status` has a `skipped` state meaning this run was skipped because "the previous one had not finished". Having its
own state matters — it explains "why did it not run today".

**Run history gets its own table** (`sys_job_logs`, see §3.11): those four fields hold only the latest run, so "how long
has this job been taking over the last ten runs" and "why did last night's run fail" have no answer. Retention is not a
separate setting — it shares `APP_LOG_RETENTION_DAYS` (30 days by default) with `sys_login_logs` / `sys_oper_logs`,
because "how long do we keep logs" is one question and should not have two answers.

### 3.11 `sys_job_logs`

Job execution history. Append-only, one row per run.

| Column | Notes |
| --- | --- |
| `job_key` | matches the code registry. **No foreign key** — a job deleted from code must keep the records of the runs it did |
| `trigger` | `cron` (fired by the scheduler) / `manual` (someone clicked "run now"). "Why did this job run twice today" is usually answered by this column |
| `status` | `success` / `failed` / `skipped`, the same values as `sys_jobs.last_status` but **without the empty string** — a row exists only when the job actually ran (or was skipped) |
| `error` | the failure or skip reason; empty on success |
| `duration_ms` | how long it took |

Same rules as the other audit tables: **no `updated_at`**, append only, never modified.
`created_at` is the run's start time, the same instant as `sys_jobs.last_run_at`.

Writing: appending the log row and updating `sys_jobs.last_*` happen in **one transaction**. Writing them separately
means a process that stops between the two writes leaves two contradictory realities — "the list says the last run
succeeded, the history has no such row".

Indexes: `(job_key, id)` for paging through one job's history, `created_at` for retention cleanup.

### 3.12 Why jobs cannot live in the database the way RuoYi's do

RuoYi stores a cron expression in the database and reflectively invokes a bean method. **That road does not exist in
Go**: there is no safe way to reflectively call an arbitrary function.

So jobs can only be registered in code, and the UI **offers no way to create one**: a job created there would never be
executed, because no code would run it. That is the same reasoning as permission points not being creatable in the UI.

At startup the code registry is **upserted** (`ON CONFLICT DO NOTHING`, so a user-edited cron is not overwritten), and a
key present in the database but not in the registry only produces a **warning** rather than refusing to start — the
opposite of how permission codes are treated: a job row that will never run is harmless, whereas an invalid permission
code makes authorization silently fail.

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

## 5. Indexes

| Index | Reason |
| --- | --- |
| `sys_users.username` | UNIQUE, for the login lookup |
| `sys_roles.code` | UNIQUE, so code can recognize built-in roles |
| `sys_menus(parent_id, sort)` | building the tree and ordering siblings |
| `sys_user_roles(role_id)` | looking up "who holds this role". The composite primary key `(user_id, role_id)` cannot cover a query on `role_id` alone |
| `sys_role_menus(menu_id)` | same |
| `sys_role_perms(perm_code)` | looking up "which roles hold this permission" |

The leftmost prefix of each composite primary key already covers the forward query, so only the reverse index is added.

## 6. SQLite specifics (all measured)

### 6.1 `timestamptz` is unavailable; `datetime` is required

`timestamptz` is a PostgreSQL type. Measured, which candidate declared types can be scanned into Go's `time.Time`:

| Declared type | Storage class | Scans into `time.Time` |
| --- | --- | --- |
| `timestamptz` | text | ❌ **cannot be scanned** |
| `timestamp` | text | ✅ |
| `datetime` | text | ✅ |
| `date` | text | ✅ |
| `time` | text | ❌ |
| `varchar` | text | ❌ |

The basis is the driver's source, `modernc.org/sqlite/sqlite.go`:

```go
switch r.ColumnTypeDatabaseTypeName(i) {   // = strings.ToUpper(the declared type)
case "DATE", "DATETIME", "TIMESTAMP":      // exact match
    dest[i], _ = r.c.parseTime(v)
default:
    dest[i] = v                            // what you get is a string
}
```

It is an **exact match**, so `timestamp(3)` and `timestamp with time zone` do not work either.
This project uses `datetime` throughout.

### 6.2 `varchar(n)` lengths are not enforced

Measured: writing 500 characters into a `varchar(10)` column **succeeds**, and 500 characters are actually stored.

SQLite ignores the length constraint; the 128 in `varchar(128)` is documentation. **Real length validation has to live
in Go's `binding` tags**:

```go
Username string `json:"username" binding:"required,min=2,max=128"`
```

Both places should be written, but know that only the latter takes effect.

### 6.3 `INTEGER PRIMARY KEY` and `BIGINT PRIMARY KEY` are not equivalent

Measured:

```
INTEGER  ✅ id is a rowid alias (rowid=9007199254740993)
BIGINT   ⚠️ id uses a separate primary key index (rowid=1, id=9007199254740993)
```

With `BIGINT PRIMARY KEY`, SQLite builds an extra primary key index B-tree, so a lookup by primary key takes two B-tree
searches. The primary key therefore **must** be declared `INTEGER PRIMARY KEY`.

### 6.4 `NULL` and `0` are two different kinds of data

Measured with `parent_id bigint NULL DEFAULT 0`:

```
INSERT DEFAULT VALUES          → parent_id = 0     (typeof=integer)
INSERT (parent_id) VALUES(NULL)→ parent_id = NULL  (typeof=null)
```

So "nullable with a default of 0" contradicts itself, and a query for "root nodes" has to handle both. This schema
allows **only one**: `parent_id` uses NULL (see §3.3), `login_at` uses NULL, and every other string field uses
`NOT NULL DEFAULT ''`.

### 6.5 Other

- A `boolean` declared type scans into Go's `bool` normally, with the `true`/`false` literals stored as the integers 1/0
- A `CHECK` constraint **really is enforced** in SQLite, which is why enums are pinned down with one
- SQLite has no `ON UPDATE CURRENT_TIMESTAMP`, so every UPDATE statement must explicitly
  `SET updated_at = CURRENT_TIMESTAMP` (in the SQL, not computed in Go, to avoid a clock difference between the
  application and the database)

## 7. The convention with frontend i18n

A menu title stores an **i18n key** (`title_key`), not text:

```
sys_menus.title_key = 'menu.system.users'
  → zh-CN: '用户管理'
  → en-US: 'Users'
```

That matches the existing architecture: **the backend emits keys only; the text belongs to the frontend** (see
[architecture.en.md](architecture.en.md)).

It is safe to do this because a menu must correspond to a frontend page component — **creating a menu necessarily means
writing a frontend page as well** — so "the user created a menu in the UI with no text behind it" cannot happen. A menu
is fundamentally deployment-time configuration (see §8.1).

Adding a menu, end to end: write a migration seeding the menu row + add the `menu.*` keys to
`web/src/lib/i18n/zh-CN.ts` and `en-US.ts`.

The labels for enum values (`status`, `menu_type`) go through the frontend i18n dictionary too, and that is why the
dictionary tables were removed. Details in [architecture-web.en.md](architecture-web.en.md).

## 8. The permission model

The settled approach: **declared in code + validated at startup**.

### 8.1 Permission codes are code constants

A permission code looks like `system:user:add` and is declared where the route is registered:

```go
users.POST("", perm(perm.SystemUserAdd), h.Create)
```

`perm.SystemUserAdd = "system:user:add"` is defined in Go code and is the single source of truth.

The permission list UI reads `GET /api/v1/perms` — an endpoint generated from the route table in code, grouped by path
prefix, which **does not touch the database**.

### 8.2 The built-in `admin` role is implicitly allowed

The role with `sys_roles.code = 'admin'` has **nothing written** into `sys_role_perms` or `sys_role_menus`; the code
decides directly:

```go
func (s *AuthzService) allowed(u *domain.User, code string) bool {
    if u.HasRole("admin") { return true }   // implicitly allowed
    return s.permCache.Has(u.ID, code)
}
```

Two reasons:

1. **It avoids a seed-ordering problem.** If the admin's permissions had to land in `sys_role_perms`, that set of
   permission codes would have to match the set declared in code exactly — but at initialization time the endpoints do
   not exist yet, so what gets written would be judged a "surplus permission code" by the startup validation of §8.3
   and refuse to start
2. **It avoids a common operational accident.** With explicit grants, one careless edit to the super-administrator
   role's permissions in the UI can lock you out of the system, and fixing it means editing the database by hand

The price is that the super-administrator role **cannot be restricted**. If you need a "limited administrator", create a
separate ordinary role.

### 8.3 ⚠️ Menu visibility and API permission are two different things

Menus and permission codes are **decoupled**. So setting a menu to `status = 'inactive'`:

- ✅ hides it from the sidebar
- ❌ **the endpoints remain callable**, as long as the role holds the matching `perm_code`

"Hiding a menu" is not "disabling a feature". To truly disable it you have to revoke the role's permission code.

### 8.4 Why permission points cannot be managed in the UI

The UI does have an "API permissions" page (`/system/apis`), but it is **read-only**. That is not laziness:

For a permission point to actually do something, three things must exist together:

1. **The declaration in code** (`internal/perm`) — determines that it appears in the list
2. **A route's reference to it** (`registrar.protect(..., code, ...)`) — determines which endpoint it guards
3. **The check at request time** (`middleware.RequirePerm`) — determines that ticking it really blocks something

Writing a single permission code row to the database provides none of the three: it guards no endpoint, and ticking it
does nothing. That is a record that "looks like a permission but is decoration", which is worse than not having it.

So the page provides **inspection** (permission code ↔ method + path), and grants stay on the role side. The `endpoints`
returned by `GET /perms` come straight from the route table assembled at startup and do not query the database — they
cannot disagree with the real endpoints.

`TestEveryDeclaredPermIsUsedByARoute` closes the other half in reverse: a declared permission code that no route
references would appear in the authorization UI while doing nothing, and the case has to fail.

### 8.5 Startup validation

At startup the service compares two sets:

1. every permission code declared in code (set A)
2. every `perm_code` present in `sys_role_perms` (set B)

**A non-empty `B - A` fails startup**, listing the surplus permission codes.

This blocks the most dangerous class of problem: after a permission code is renamed, the old grant records in the
database neither error nor take effect — they silently drop permissions (or worse, silently grant them once the code is
reused).

## 9. Decision record

### 9.1 Whole tables removed

| Table | Reason |
| --- | --- |
| `sys_depts` | **Zero consumers**. The original `sys_roles` had no `data_scope` field, so departments played no part in data permissions, and no other table or feature reads them. If they are needed later, adding a table is safer than changing one |
| `sys_dicts` + `sys_dict_datas` | The frontend i18n dictionary already covers the main use, enum labels. A database-driven dictionary costs extra: two CRUD modules, cache invalidation logic, and the "the dictionary changed but the frontend did not refresh" class of problem |
| `sys_apis` | Once permission codes became code constants, `(api_path, api_method)` was redundant data derived from code. Keeping it would cost more: an id space, an upsert sync at startup, path-parameter pattern matching, and the risk of silently failing when the database and the code disagree |

### 9.2 Whole classes of fields removed

| Field | Occurrences | Reason |
| --- | --- | --- |
| `created_by` `updated_by` | 14 columns | No consumers at all (not displayed, not read); `NOT NULL` forces seed data to write fake values (there is no logged-in user at initialization time); and threading the current user from handler to repository would add a parameter to every repository method |
| `tenant_id` | 2 columns | no multi-tenancy |
| `deleted_at` `deleted_by` | 2 columns | replaced by deactivating via `status`. The hidden cost of soft deletes: every SELECT has to carry `IS NULL` (miss one and it is a silent data leak), and unique indexes have to be rewritten as partial indexes |

### 9.3 Individual columns removed

| Table.column | Reason |
| --- | --- |
| `sys_users.position` | there is no job-title system; pure decoration |
| `sys_users.dept_id` | removed along with `sys_depts` |
| `sys_menus.is_redirect` `redirect` | the frontend auto-navigates when a directory has a single child; no need to store two fields on the backend |
| `sys_menus.remark` | the menu name (`title_key`) already says what it is |
| `sys_apis.*` | removed along with the table |
| `sys_dicts.remark` `sys_dict_datas.remark` | removed along with the tables |

### 9.4 Fields changed

| Before | After | Reason |
| --- | --- | --- |
| `id bigint` | `id INTEGER PRIMARY KEY AUTOINCREMENT` | a snowflake id is 64-bit (about 1.8e18), past JS's `Number.MAX_SAFE_INTEGER` (9007199254740991), so the frontend's `JSON.parse` **silently truncates** it and sending it back guarantees a 404. And on a single machine with SQLite, snowflake ids bring no benefit anyway |
| `timestamptz` | `datetime` | measured: cannot be scanned into `time.Time` (§6.1) |
| `status` with three states | two states | `suspended` has no producer anywhere in the schema (there is no login-failure counter and no locking mechanism) |
| `menu_type` with three states (including button) | two states | the `button` type's job was taken over by permission codes |
| `sys_menus.name` | `sys_menus.title_key` | a menu must correspond to a frontend page and is fundamentally deployment-time configuration; storing a string would lock the menu to one language, conflicting with the bilingual frontend already built |
| `sys_role_apis` | `sys_role_perms` | no longer references `sys_apis.id`; stores the permission code directly |

### 9.5 Fields added

| Field | Reason |
| --- | --- |
| `sys_roles.code` | code has to recognize the built-in administrator role, and `id` is an auto-increment value that differs between environments |
| `sys_roles.remark` | absent from the original design. Roles are few and long-lived, so there needs to be somewhere to record "why does this role exist" |
| `sys_menus.sort` | menu order is something users perceive directly, and ordering by auto-increment id is a coin flip |

## 10. Seed data

Seeded by `migrations/0003_seed_rbac.sql`:

| Content | Notes |
| --- | --- |
| the built-in role `code = 'admin'` | the super administrator, implicitly allowed everything (see §8.2) |
| the `admin` user | the bcrypt hash of the password `admin123`, **hardcoded in the migration** |
| the `admin` user's `sys_user_roles` row | |
| the menu tree | three directories (system management / system monitoring / tools) plus 10 page menus, seeded in five rounds |

**Nothing is written to `sys_role_perms` / `sys_role_menus`** — see §8.2.

Menus are seeded in rounds to accommodate existing databases: adding a menu leaves the old migrations alone and appends a
new one. Directories are wrapped in `WHERE NOT EXISTS`, so re-running on an old database does not produce a second copy,
while page menus are inserted directly with the directory as their parent.

| Migration | Menus |
| --- | --- |
| `0003_seed_rbac.sql` | system management: users / roles / menus |
| `0005_seed_api_menu.sql` | system management: permission list (a read-only page; the reason is in §8.4) |
| `0007_seed_monitor_menu.sql` | system monitoring (a new directory): online sessions / login logs / operation logs |
| `0009_seed_tool_menu.sql` | tools (a new directory): file management / scheduled jobs |
| `0011_seed_system_monitor_menu.sql` | system monitoring: service status (inserted first, with the other three's `sort` shifted down in turn) |

### 10.1 The initial password

A **weak password hardcoded in the migration**, `admin123`. The trade-off:

- Open-source users can log in right after cloning, with no extra steps
- The price is a weak password, and you cannot count on every user changing it

**There is no "force a password change at first login"**. Adding it would require a new field (such as
`must_change_password`) plus a set of interception logic — an extra feature.

The bcrypt hash in the migration is **transcribed by hand**, and one wrong character means nobody can log in. So
`TestSeedRbac` in `migrate_test.go` asserts with `bcrypt.CompareHashAndPassword` that it really corresponds to
`admin123`. That case is not there for coverage; it is there to catch a transcription error — and it has in fact caught a
missing character once.

## 11. Not defined yet

| Item | Notes |
| --- | --- |
| Login-failure lockout | If added, `status` may need `suspended` back, or a new `locked_until` field |
| Data permissions (visibility by department or by person) | Not done today. If it is, "departments" have to be reintroduced and `sys_roles` needs a `data_scope` |

The session table (§3.7) and the log tables (§3.8) were once listed here; they have landed, so they are no longer
mentioned.