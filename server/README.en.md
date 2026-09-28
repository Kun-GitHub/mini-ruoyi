# mini-ruoyi / server

English | [简体中文](README.md)

The backend service. Go 1.25 + Gin + `database/sql` + SQLite (`modernc.org/sqlite`, pure Go, no CGO).

The module name is `mini-ruoyi`. SQLite is the only database backend; there is no multi-dialect abstraction.

## Requirements

- Go 1.25 or newer (`go.mod` declares `go 1.25`; with `GOTOOLCHAIN=auto` the matching toolchain is downloaded automatically)
- No CGO, no SQLite command-line tool

## Quick start

```bash
cd server

# run it directly (if there is no web/ in the current directory or next to the binary, it falls back to API-only mode)
go run ./cmd/server

# verify
curl localhost:8080/healthz
# {"code":200,"msg":"ok","data":{"status":"up"}}
```

Building the complete artifact (frontend included) from the repository root:

```bash
make build && make run       # the binary and the frontend output land in bin/
```

At startup any unapplied migrations run automatically and `data.db` is created if it does not exist.

The `data.db` that ships in the repository is an **already initialized** database: every migration is recorded
(`0002`–`0011` all applied) and its contents are identical to a freshly migrated one — one `admin` user, one built-in
role, 13 menus and 3 jobs, with **no sessions, no logs and no test accounts**. It works out of the box.

⚠️ **This same database is also the live development database** (when you start the backend from `server/`,
`database.path` points at it). A single login writes sessions and logs into it, and they get committed along with
everything else — which shows up as "I cloned this and the online-sessions page lists someone else's records".
Run **`make db-clean`** before committing to restore it.

The eventual fix is to rebuild it from `migrations/` + seed SQL (see
[../docs/architecture.en.md](../docs/architecture.en.md) §8); at that point the database goes away and so does this
caveat.

## Configuration

**Precedence: environment variables > config file > built-in defaults.**

```bash
cp config/config.example.yaml config/config.yaml   # optional; it runs without this
```

`config.yaml` is in `.gitignore`; the committable sample is `config/config.example.yaml` and every entry in it is
commented. `APP_CONFIG` points at a different path.

| Variable | Default | Where it lives in the config file |
| --- | --- | --- |
| `APP_CONFIG` | `config/config.yaml` | — (the config file path itself) |
| `APP_ADDR` | `:8080` | `server.addr` |
| `APP_ENV` | `prod` | `server.env` |
| `APP_SECURE_COOKIE` | `false` | `server.secure_cookie` |
| `APP_TRUSTED_PROXIES` | empty | `server.trusted_proxies` (comma-separated) |
| `APP_RATE_LIMIT_RPS` | `20` | `server.rate_limit.rps` |
| `APP_RATE_LIMIT_BURST` | `40` | `server.rate_limit.burst` |
| `APP_DB_PATH` | `data.db` | `database.path` |
| `APP_WEB_DIR` | auto-detected | `web.dir` |
| `APP_UPLOAD_DIR` | `uploads` | `upload.dir` |
| `APP_UPLOAD_MAX_MB` | `20` | `upload.max_mb` |
| `APP_UPLOAD_QUOTA_MB` | `512` | `upload.quota_mb` |
| `APP_LOG_RETENTION_DAYS` | `30` | `log.retention_days` |

**Bad configuration refuses to start**: an unknown field in the config file, an invalid environment variable value, or a
self-contradictory combination such as "the quota is smaller than the per-file limit" all fail at startup and point at
the exact location. If it silently fell back to defaults, all the user would feel is "I configured it, why isn't it
taking effect".

The startup log has one line summarizing the effective configuration, with absolute paths everywhere.

## API

Everything lives under `/api/v1`. The full contract (envelope, error keys, delete confirmation) is in
[../docs/architecture.en.md](../docs/architecture.en.md).

At startup it prints the current endpoint breakdown:

```
已注册 38 个 API 端点（公开 1 / 仅登录 4 / 需权限 33）
```

**How authentication works**: after login the server hands out an `mr_session` cookie (HttpOnly / SameSite=Lax).
Apart from login, every write operation must carry the `X-CSRF-Token` header, with the value taken from the login
response or from the `csrf_token` field of `/auth/me`.

### Endpoint list

| Method | Path | Permission code | Notes |
| --- | --- | --- | --- |
| POST | `/auth/login` | — **(public)** | log in; hands out the cookie and the CSRF token |
| GET | `/auth/me` | login only | current user + permission codes + menu tree + CSRF token |
| POST | `/auth/logout` | login only | log out; the server deletes the session immediately |
| PUT | `/profile` | login only | change **your own** nickname/mobile/email. You cannot change status — that would amount to letting yourself undo a suspension |
| PUT | `/profile/password` | login only | change **your own** password. Requires the old password, and only kicks the other sessions |
| GET | `/menus` | `system:menu:list` | the full menu tree (including disabled entries) |
| GET | `/menus/:id` | `system:menu:list` | |
| POST | `/menus` | `system:menu:add` | |
| PUT | `/menus/:id` | `system:menu:edit` | |
| DELETE | `/menus/:id` | `system:menu:delete` | supports `?cascade=true` |
| GET | `/roles` | `system:role:list` | paginated |
| GET | `/roles/:id` | `system:role:list` | |
| GET | `/roles/:id/grants` | `system:role:list` | menu grants + permission codes |
| POST | `/roles` | `system:role:add` | |
| PUT | `/roles/:id` | `system:role:edit` | |
| PUT | `/roles/:id/grants` | `system:role:edit` | overwrites the grants wholesale |
| DELETE | `/roles/:id` | `system:role:delete` | supports `?cascade=true` |
| GET | `/perms` | `system:perm:list` | the permission list, grouped by resource, **including the endpoints each permission code guards** (from the route table assembled at startup; no database query) |
| GET | `/users` | `system:user:list` | paginated |
| GET | `/users/:id` | `system:user:list` | includes `role_ids` |
| POST | `/users` | `system:user:add` | |
| PUT | `/users/:id` | `system:user:edit` | the username is immutable |
| PUT | `/users/:id/roles` | `system:user:edit` | |
| PUT | `/users/:id/password` | `system:user:resetPwd` | **immediately kicks all of that user's sessions** after a reset |
| DELETE | `/users/:id` | `system:user:delete` | |
| GET | `/system` | `monitor:system:list` | this machine's CPU / memory / disk / Go process state |
| GET | `/sessions` | `monitor:session:list` | online sessions (unexpired), most recently active first |
| DELETE | `/sessions/:hash` | `monitor:session:kick` | kick one session; the other side is invalidated on its next request |
| DELETE | `/users/:id/sessions` | `monitor:session:kick` | force-log-out all of one user's sessions |
| GET | `/login-logs` | `monitor:loginlog:list` | login logs, filterable by `username` / `status` |
| GET | `/oper-logs` | `monitor:operlog:list` | operation logs, filterable by `username` / `method` / `path` |
| GET | `/files` | `tool:file:list` | file list; the response also carries quota usage |
| GET | `/files/:id/download` | `tool:file:list` | download. **Forced save**; no inline preview |
| POST | `/files` | `tool:file:upload` | multipart upload, field name `file`, optional `group` |
| DELETE | `/files/:id` | `tool:file:delete` | deletes the row and the file on disk |
| GET | `/jobs` | `tool:job:list` | scheduled jobs (from the code registry) |
| PUT | `/jobs/:key` | `tool:job:edit` | change the cron / enable-disable; **rescheduled immediately** |
| POST | `/jobs/:key/run` | `tool:job:run` | run once now (asynchronously; refresh the list to see the result) |
| GET | `/healthz` | — **(public)** | liveness; really pings the database |

### "Your own" and "someone else's" are two separate sets of endpoints

| | changing your own | changing someone else's |
| --- | --- | --- |
| profile | `PUT /profile` (login only) | `PUT /users/:id` (needs `system:user:edit`) |
| password | `PUT /profile/password` (login only, **old password required**) | `PUT /users/:id/password` (needs `system:user:resetPwd`) |

The two "your own" endpoints **require no permission code at all**: the initial password was set by an administrator, so
if changing your own password also required `system:user:resetPwd`, an account granted read-only access could not even
change its own password. They go through the `self` routes (login check only).

Two differences are deliberate:

- Changing your own password **must verify the old password** — otherwise a hijacked session could simply change the
  password and lock the owner out
- Changing your own password **kicks only the other sessions**, keeping the current one. Logging yourself out
  immediately after the change would make the user think the change failed

### Logging in

```bash
curl -c cookie.txt -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'
```

```json
{"code":200,"msg":"ok","data":{
  "user":{"id":1,"username":"admin","nickname":"管理员", ...},
  "is_admin":true,
  "perms":["system:menu:add", "..."],
  "csrf_token":"Xce5DgS3...",
  "expires_at":"2026-10-04T08:45:15Z",
  "menus":[{"title_key":"menu.system","children":[...]}]
}}
```

The built-in administrator (role `code='admin'`) returns **every declared permission code** in `perms`, so the frontend
only needs one kind of check: `perms.includes(code)`. Its permissions are implicit and never written to the database;
see [../docs/schema.en.md](../docs/schema.en.md) §8.2.

### Deleting a resource that has dependents

Without `?cascade=true`, a resource that has child data returns `409` carrying the impact:

```bash
curl -b cookie.txt -X DELETE localhost:8080/api/v1/menus/1
```

```json
{"code":409,"msg":"error.hasDependents",
 "data":{"child_menus":3,"affected_roles":2}}
```

When the frontend receives it, **it does not show an error toast — it shows a confirmation dialog** with those numbers
displayed; once the user confirms, it re-sends `DELETE .../menus/1?cascade=true`. The impact fields for each resource
are in [../docs/architecture.en.md](../docs/architecture.en.md) §3.4.

### Error responses

On a failing response `msg` is an **i18n key**, not text; the frontend renders the text.

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
| 404 | `error.frontendDisabled` (hitting a non-API path in **API-only mode**, telling you the frontend is not deployed) |

The backend does **not** return `error.network` or `error.backendUnreachable` — the frontend produces those (the former
means nothing answered the request; the latter means something answered but it was not this service's envelope format).

Built-in limits: request body up to 1 MiB, rate limiting per IP at 20 rps (burst 40).
Audit logs are written in batches, so there is **up to a 2-second delay** (why: [../docs/architecture.en.md](../docs/architecture.en.md) §5.2).
Retention is controlled by `APP_LOG_RETENTION_DAYS`, 30 days by default.

## Database

### Adding a migration

1. Create `<sequence>_<description>.sql` in `internal/repository/migrations/`, with the sequence zero-padded to 4 digits:

   ```
   internal/repository/migrations/0002_init_users.sql
   ```

2. Write plain SQL; one file may contain several statements:

   ```sql
   CREATE TABLE users (
       id       INTEGER PRIMARY KEY AUTOINCREMENT,
       username TEXT NOT NULL UNIQUE,
       ...
   );
   CREATE INDEX idx_users_username ON users(username);
   ```

3. Restart the service and it is applied automatically.

Rules:

- **Forward only, no rollback.** If you got it wrong, add a repair migration
- Each file runs in **a single transaction**, so a failing DDL statement does not leave half a structure behind
- Applied files are recorded in `schema_migrations` and skipped. Restarting repeatedly is idempotent
- Migration files are packaged into the binary with `//go:embed`, so they **must ship with the code**; operations cannot
  drop in SQL separately

### Things to watch when changing the schema

Migrations can only **append**: versions live in `schema_migrations`, only unrun files execute, and nothing is rebuilt or
overwritten — otherwise upgrading an old database (tables present, possibly with no migration records at all) fails.
See the cases `TestMigrateOnLegacyDatabase` (legacy database with data, no migration records) and
`TestMigrateDetectsMissingTables` (the records say it ran, but the table is gone).

### SQLite connection configuration

`PRAGMA`s are delivered through the DSN's `_pragma` parameter. **Do not change this to
`db.Exec("PRAGMA ...")`** — that is a connection-level setting, and `db.Exec` configures only one connection in the
pool while the rest keep the defaults (`foreign_keys=0`, `busy_timeout=0`). Details in
[../docs/architecture-server.en.md](../docs/architecture-server.en.md).

`repository/sqlite_test.go` has the matching regression case; it must pass after any change to the connection
configuration.

## Tests

```bash
go test ./...                                    # everything
go test ./internal/repository/ -v                # one package
go test ./... -run TestNewDBConfiguresPragmas    # one case
gofmt -l . && go vet ./...                       # before committing
```

The coverage of each case is in the "Tests" section of
[../docs/architecture-server.en.md](../docs/architecture-server.en.md).

## Code map

```
cmd/server/main.go                 startup, dependency wiring, graceful shutdown
internal/config/                   environment variables → Config
internal/domain/                   entities + domain errors (the innermost layer)
internal/httpx/                    response envelope, error keys, error→status-code mapping
internal/repository/               connections, migrations, single-table SQL
internal/service/                  business rules, page normalization
internal/handler/                  the HTTP adapter: binding, validation, responses
internal/middleware/               logging, rate limiting, body limit, cache headers
internal/httpserver/               route assembly, static asset hosting + SPA fallback
```

The dependency direction is strictly one-way: `handler` is allowed to import `repository` only to assemble list
filters (the `Filter` family); it must not write SQL and must not make business decisions. Details in
[../docs/architecture-server.en.md](../docs/architecture-server.en.md).

The complete 8-step recipe for adding a resource is in that same document.

## Not implemented yet

- **Login-failure lockout**: today only a login log is written; failures are not counted and accounts are not locked
- **Job execution history**: `sys_jobs` holds only the most recent run's result; there is no history table
- **List export**: users/roles/logs have no export endpoint

The `devices` sample resource has been removed: under a fail-closed permission model it would either pollute the
production permission list or leave behind a permissionless CRUD endpoint, and neither belongs in a release.