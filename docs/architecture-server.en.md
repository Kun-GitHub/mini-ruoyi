# Backend architecture

English | [简体中文](architecture-server.md)

Go 1.25 + Gin 1.10 + `database/sql` + SQLite (`modernc.org/sqlite`, pure Go, no CGO). Zero ORM, zero extra services.

## 1. Directory layout

```
server/
├── go.mod                          # module mini-ruoyi
├── data.db                         # the initial database shipped in the repository
├── config/config.example.yaml      # annotated config sample (config.yaml itself is gitignored)
├── cmd/server/main.go              # startup, dependency wiring, graceful shutdown
└── internal/
    ├── config/config.go            # config struct and loading (file + environment)
    ├── domain/                     # entities + domain errors (innermost layer, depends on nothing)
    │   ├── consts.go               # statuses, menu types, built-in role codes
    │   ├── errors.go               # error sentinels + DependentsError / DuplicateError
    │   ├── user.go · rbac.go       # User; Role / Menu / MenuNode
    │   ├── session.go · log.go     # Session / SessionView; LoginLog / OperLog / JobLog
    │   └── file.go · job.go        # File / FileUsage; Job
    ├── httpx/response.go           # the response envelope, error keys, error → HTTP status mapping
    ├── perm/perm.go                # permission code constants and grouping; no database access
    ├── auth/                       # password hashing, session issuing and parsing
    ├── repository/
    │   ├── sqlite.go               # connection and pool
    │   ├── migrate.go              # the migration runner
    │   ├── time.go · filter.go     # time storage format; LIKE escaping and WHERE assembly
    │   ├── migrations/*.sql        # versioned DDL + seed data
    │   └── *_repository.go         # single-table CRUD: user / role / menu / session / log / file / job
    ├── service/                    # business rules, transaction boundaries, pagination normalization
    │   ├── paging.go               # Page[T] and page normalization
    │   ├── authz_service.go        # permission decisions (an administrator holds every permission code)
    │   ├── log_service.go          # audit log buffering and batch writes
    │   └── *_service.go            # user / role / menu / file / job / monitor
    ├── system/                     # collects CPU/memory/disk/process (read-only OS probing, no library)
    │   ├── system.go               # Snapshot assembly and /proc parsing
    │   └── disk_unix.go · disk_windows.go   # Statfs vs GetDiskFreeSpaceExW
    ├── job/                        # job.go is the registry; defs.go holds the job definitions
    ├── handler/                    # HTTP adaptation: binding, validation, responses
    ├── middleware/                 # auth.go for sessions and CSRF; operlog.go for auditing; recovery.go wraps panics in the envelope
    └── httpserver/
        ├── router.go               # route registration, middleware assembly
        ├── registrar.go            # route registration entry points (public / self / protected)
        └── static.go               # static hosting from disk + SPA fallback
```

## 2. Layering and dependency direction

Dependencies are strictly one-way, with no cycles:

```
domain      → (depends on no internal package)
httpx       → domain
perm        → (depends on no internal package)
auth        → domain, repository
repository  → domain
service     → auth, domain, perm, repository
handler     → domain, httpx, middleware, perm, repository, service
middleware  → auth, domain, httpx, perm, service
httpserver  → handler, httpx, middleware, perm
```

| Layer | Responsibility | Forbidden |
| --- | --- | --- |
| `domain` | Entity definitions, domain error sentinels (`ErrNotFound` / `ErrHasDependents` / `ErrDuplicate` and friends) | Depends on any other internal package |
| `perm` | Permission code constants and grouping, no database access | Depends on any other internal package |
| `auth` | Password hashing/verification, session issuing and parsing | Depends on handler / service |
| `httpx` | The response envelope, error keys, error → status mapping | Depends on handler / service / repository |
| `repository` | Single-table SQL, rows ↔ entities | Business rules, cross-table transaction orchestration |
| `service` | Business rules, transaction boundaries, pagination normalization | Touching `*gin.Context`, building HTTP responses |
| `handler` | Parameter binding, calling services, writing responses | Writing SQL or business rules (errors travel only as `domain` sentinels) |
| `middleware` | Cross-cutting concerns | Depends on handler |
| `httpserver` | Route and middleware assembly, static assets | Business logic |

Two hard rules:

1. **Errors always travel as `domain` sentinels** (`ErrNotFound` / `ErrHasDependents` / `ErrDuplicate` …), and the
   status mapping lives in exactly one place, `httpx.FailFromError`; no `switch` over status codes belongs in a handler.
   Handlers do import `repository`, but only to assemble list filters (the `repository.Filter` family) — they write no SQL
   and make no business decisions. That import used to be a genuine layering leak (handlers writing their own queries);
   it has been fixed.
2. **The response envelope lives in `httpx`, not `handler`.** Middleware and httpserver write responses too
   (401/403/404/429/503), so if the envelope lived in handler, those packages would have to depend on handler backwards.

## 3. Startup flow

`cmd/server/main.go`:

```
config.Load()                        read environment variables
  ↓
gin.SetMode(Debug/Release)          decided by APP_ENV
  ↓
repository.NewDB(cfg.DBPath)         open the connection + Ping (a failed Ping exits; config errors surface early)
  ↓
repository.Migrate(ctx, db)          run unapplied migrations in order
  ↓
manual wiring of repo → service → handler     no wire/fx
  ↓
httpserver.NewRouter(...)            routes + middleware; an incomplete frontend directory is an error
  ↓
ListenAndServe + wait for SIGINT/SIGTERM
  ↓
srv.Shutdown(15s)                    graceful shutdown
```

Timeout settings (`main.go` constants):

| Setting | Value | Notes |
| --- | --- | --- |
| `readTimeout` | 5s | Deadline for reading the whole request including the body; also covers header reading, which guards against Slowloris |
| `writeTimeout` | 10s | Deadline for writing the response |
| `idleTimeout` | 60s | Keep-alive idle deadline |
| `shutdownTimeout` | 15s | **Must exceed `writeTimeout`**, or in-flight responses get cut off |

## 4. The middleware chain

Assembly order is execution order (`httpserver/router.go`):

```
1. middleware.Recovery()                            panic → 500 (still the envelope, not an empty body)
2. middleware.RequestLogger()                       method path status duration
3. SetTrustedProxies(APP_TRUSTED_PROXIES)           ← not middleware, but router config, and it must precede rate limiting
4. middleware.BodyLimit(1 MiB)                      request body cap (excluding /api/v1/files)
5. middleware.RateLimit(20 rps, burst 40, TTL 3m)   per-IP rate limit
6. middleware.ImmutableAssets("/assets/")           long-lived cache headers for static assets
   ── routes ──
   GET  /healthz                                     → SQLite Ping
   /assets/*filepath
   /api/v1   7→8→9 apply to the whole group; 10 is attached only to routes registered as protected
   │   7. Auth.Require()                             no session → 401
   │   8. Auth.CSRF()                                write request without a token → 403
   │   9. middleware.OperationLog(...)                writes and 401/403 are audited
   │  10. middleware.RequirePerm(code)                missing permission code → 403
   └── menus / roles / users / permission list / files / jobs / monitoring / sessions / logs
   ── NoRoute ──
   /api/* and /assets/* → JSON 404; everything else → index.html (SPA fallback)
```

Three ordering constraints — read this before touching the assembly order:

| Constraint | Why |
| --- | --- |
| `SetTrustedProxies` before `RateLimit` | The limiter counts by `c.ClientIP()`, so configuring proxies afterwards is the same as not configuring them |
| `Require` before `CSRF` | Establish "who" first; only then does a token have an owner to be compared against |
| `OperationLog` before `RequirePerm` | Permission probing (403) has to be recorded too |

`RequirePerm` is attached per protected route rather than via `r.Use`: permission codes are inherently per-route, and
attaching them at assembly time makes omission impossible. `httpserver/registrar.go` has only three registration entry
points — `open` (which must state "why this needs no login", and the reason is asserted by a test), `self` (login only,
operating on your own data), and `protect` (login + a permission code; the code must have been declared in `internal/perm`,
and a wrong one panics at startup).

### 4.1 Route list

39 endpoints today: **1 public + 4 self-service + 34 permission-protected** (the `/api/v1` prefix is omitted below).

| Group | Endpoints |
| --- | --- |
| Public | `POST /auth/login` |
| Self-service (`self`) | `GET /auth/me`, `POST /auth/logout`, `PUT /profile`, `PUT /profile/password` |
| Menus | `GET /menus`, `GET /menus/:id`, `POST /menus`, `PUT /menus/:id`, `DELETE /menus/:id` |
| Roles | `GET /roles`, `GET /roles/:id`, `GET /roles/:id/grants`, `POST /roles`, `PUT /roles/:id`, `PUT /roles/:id/grants`, `DELETE /roles/:id` |
| Users | `GET /users`, `GET /users/:id`, `POST /users`, `PUT /users/:id`, `PUT /users/:id/roles`, `PUT /users/:id/password`, `DELETE /users/:id` |
| Permission list | `GET /perms` |
| Files | `GET /files`, `GET /files/:id/download`, `POST /files`, `DELETE /files/:id` |
| Jobs | `GET /jobs`, `PUT /jobs/:key`, `POST /jobs/:key/run`, `GET /jobs/:key/logs` |
| Monitoring | `GET /system`, `GET /sessions`, `DELETE /sessions/:hash`, `DELETE /users/:id/sessions`, `GET /login-logs`, `GET /oper-logs` |

Read and write permission codes are declared separately: "can view" implying "can modify" is a common authorization
hole, so `GET /roles/:id/grants` uses the `list` code while `PUT /roles/:id/grants` uses the `edit` code, and
`POST /jobs/:key/run` (run once now) and `PUT /jobs/:key` (change the schedule) each get their own.

Conversely, **read endpoints reuse an existing code**: run history hangs off the jobs' `list` code. "What did that
job do last time" is already on the list page, so a second code would mean existing roles suddenly lose sight of
something they could see before.

### 4.2 Two easy traps

**Rate limiting must be per IP, never process-wide.** A process-wide token bucket means one client exhausting the quota
turns every other user's requests into 429s — the limiter becomes a DoS amplifier. The limiter's map must also be GC'd
lazily, or a port scan grows it unbounded with every IP ever seen.

**`X-Forwarded-For` must not be trusted.** This service listens publicly with no nginx in front. Without calling
`SetTrustedProxies(nil)`, gin's `ClientIP()` honours a client-supplied `X-Forwarded-For` and the rate limit can be
bypassed with one header line. `TestRateLimitIgnoresSpoofedForwardedFor` covers this.

## 5. The response envelope (`internal/httpx`)

```go
type Response struct {
    Code   int          `json:"code"`             // always the HTTP status code
    Msg    string       `json:"msg"`              // "ok" on success; an i18n key on failure
    Data   any          `json:"data,omitempty"`
    Errors []FieldError `json:"errors,omitempty"`
}

type FieldError struct {
    Field string `json:"field"`           // JSON field name
    Rule  string `json:"rule"`            // validator tag: required / min / max / ...
    Param string `json:"param,omitempty"` // rule parameter
}
```

**The backend produces no user-facing text**, only stable i18n keys (`error.notFound` and friends); the frontend renders
them. That is what makes multiple languages possible: the moment the backend hardcodes Chinese, the frontend can no
longer translate it.

### 5.1 Two error mapping entry points

They are two functions because their semantics differ:

| Function | Handles | Mapping |
| --- | --- | --- |
| `FailBindError` | request body parsing/validation failure | `*http.MaxBytesError` → 413; `validator.ValidationErrors` → 400 + `errors[]`; anything else → 400 `error.malformedBody` |
| `FailFromError` | domain/business errors | `domain.ErrNotFound` → 404; anything else → 500 `error.internal` + **logged** |

The 5xx branch of `FailFromError` must log: the client only receives an `error.internal` key, so server-side logs are the
only way to investigate.

### 5.2 Field names come from JSON tags

`RegisterJSONFieldNames()` is called from `NewRouter` and registers a tag-name function with gin's global validator, so
validation errors carry `name` rather than `Name`. It must be called before routes are registered, and only once.

## 6. The data layer

### 6.1 Connection settings (`repository/sqlite.go`)

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

### ⚠️ Why the PRAGMAs have to go through the DSN

`busy_timeout` / `foreign_keys` / `synchronous` are all **per-connection** settings. Running
`db.Exec("PRAGMA ...")` only hits **one** connection from the pool; every other connection keeps the defaults.
Measured with a pool cap of 4:

```
conn#0 foreign_keys=1 busy_timeout=5000 synchronous=1   ← the only one that got configured
conn#1 foreign_keys=0 busy_timeout=0    synchronous=2
conn#2 foreign_keys=0 busy_timeout=0    synchronous=2
conn#3 foreign_keys=0 busy_timeout=0    synchronous=2
```

The consequence is that 3 out of 4 requests hit `SQLITE_BUSY` immediately on a write lock, and foreign key constraints
are nominal.

`journal_mode=WAL` is the one exception — it is written into the database header, so it is a persistent setting and
happens to work either way. It lives in the DSN too, to keep the behaviour in one place.

**Regression test**: `TestNewDBConfiguresPragmasOnEveryConn` in `repository/sqlite_test.go` holds all four connections at
once and asserts each one individually. **Any change to connection settings has to keep that test passing.**

Pool cap of 4: SQLite is a single-writer model, writes are serialized anyway, and extra connections only serve concurrent
reads.

### 6.2 Migrations (`repository/migrate.go`)

- Files are named `<version>_<description>.sql` and live in `internal/repository/migrations/`
- They are bundled into the binary with `//go:embed migrations/*.sql` (migrations have to be versioned with the code, never copied in by an operator)
- They run in filename order (zero-padded numeric prefixes make lexicographic order the version order)
- Each file runs its migration body **inside a single transaction** together with the `schema_migrations` row, so either both take effect or neither does
  (SQLite's DDL is transactional, which is why a failed DDL leaves no half-built schema behind)
- **Migrations only move forward; there is no rollback.** A SQLite DDL rollback script tends to disagree with the real data, so a mistake is fixed by adding another migration

```sql
CREATE TABLE schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### 6.3 Time fields

DATETIME can be **scanned straight into `time.Time`** (the driver does the conversion; no `_time_format` parameter
needed). So time fields on entities are declared as `time.Time` and JSON serialization emits RFC 3339 UTC for free.

Do not use `string`: SQLite stores values like `2025-09-26 22:04:00`, which Safari's `new Date()` cannot parse.

### 6.4 Empty lists

`List` uses `make([]T, 0, limit)` rather than `var list []T`, so an empty result serializes as `[]` instead of `null`.
Otherwise the frontend receives `data.list === null` and crashes. Unpaginated queries such as the menu tree use
`make([]Menu, 0)` — the `0` length is what matters, not the capacity.

## 7. Static asset hosting (`httpserver/static.go`)

The frontend output is read from disk and **not embedded in the binary**, which is why rebuilding the frontend needs no
backend restart.

### 7.1 Two deployment shapes

| State of `APP_WEB_DIR` | Behaviour |
| --- | --- |
| the whole directory is missing | print a warning, fall back to **API-only mode**, and return a JSON 404 for every non-API path |
| the directory exists but lacks `index.html` or `assets/` | **fail at startup** with a clear message |
| the directory is complete | mount `/assets` and fall back to `index.html` for everything else |

The first two are distinguished to serve both "I only want the API" and "I deployed the frontend wrong" — and the latter
has to fail early.

### 7.2 Routing and caching

```
/assets/*   http.FileServer(http.Dir(webDir/assets))  + Cache-Control: immutable
everything else     index.html                        + Cache-Control: no-cache
/api/*      no fallback; returns a JSON 404
```

`/api/*` and `/assets/*` **must never** fall back to `index.html`, or the frontend ends up parsing a whole page of HTML
as JSON and the error says nothing about the real cause. `TestAPINotFoundReturnsJSON` and `TestMissingAssetIsNotHtml`
cover this.

> Note: a 404 under `/assets/` is produced by gin's `createStaticHandler`, which swaps `c.handlers` for `noRoute` when
> `fs.Open` fails, so it really does reach the NoRoute branch rather than being dead code.

## 8. Configuration (`internal/config`)

Load order: **built-in defaults → config file → environment variables**, then validation.
The reasoning behind that is in [architecture.en.md](architecture.en.md) §5.5.

The config file defaults to `config/config.yaml` (changeable via `APP_CONFIG`) and **is not required** — that is what makes
clone-and-run work. Parsing uses `KnownFields(true)`, so a mistyped field name is an error rather than being ignored.

**The complete list of environment variables and config settings is in [../server/README.en.md](../server/README.en.md)** —
it is not copied here, because two copies of that table inevitably drift apart.

Two path resolution rules deserve their own explanation, and they are **deliberately different**:

| Setting | Resolution rule | Why |
| --- | --- | --- |
| `database.path` | relative to the **process working directory** | The database often lives on a data disk or a mounted volume; its location is a deployment decision and should not follow the binary around |
| `web.dir` (auto-detected when empty) | next to the binary `web/` → `./web` → `../bin/web` → `bin/web` | Frontend output naturally ships alongside the binary. The multiple candidates exist to cover `go run` and IDE launches, where the binary sits in a temp directory with no `web/` next to it |

When the frontend directory cannot be found, the service falls back to API-only mode and prints one line saying how to fix
it. Early versions looked only next to the binary and degraded silently — which showed up as "opening :8080 gives me a JSON
404" and was very hard to trace back to the frontend directory.

## 9. Tests

```bash
cd server && go test ./...        # everything
go test ./... -run TestNewDB      # one test
```

There are 127 cases across 18 test files today. `handler` / `job` / `auth` / `domain` / `cmd/server` have no test files of
their own — their correctness is covered by the end-to-end cases in `httpserver` (essentially every branch of a handler
maps to one HTTP assertion).

| File | Coverage |
| --- | --- |
| `repository/sqlite_test.go` | **the PRAGMAs take effect on every connection** (guarding against the regression above), WAL is enabled |
| `repository/migrate_test.go` | migration idempotence; upgrading a legacy database (tables but no migration records) without losing data; **the seeded password hash passes a bcrypt check**; **schema verification detects "the record says applied but the table is gone"** |
| `repository/menu_seed_test.go` | **the menu seed matches the frontend**: every menu's `component` has a page, its `title_key` exists in both dictionaries, no duplicated paths — and in reverse, no page is unreachable from the menu |
| `repository/filter_test.go` | LIKE wildcard escaping (`%` / `_` / `\`), user input never treated as a wildcard, filters really matching the expected rows |
| `service/rbac_test.go` (including identity resolution in `authz_service`) | delete impact (including grants on descendant menus), cascade delete, cycle-reference rejection, four delete guards, administrator identity resolution, pagination edges |
| `perm/perm_test.go` | group coverage and uniqueness, key-naming convention, detection of unknown permission codes, frontend dictionary coverage |
| `repository/session_repository_test.go` | **the datetime storage format**, time round-trips, expiry cleanup, kicking by user, cascading on user delete |
| `httpx/response_test.go` | error keys unique and correctly named, frontend dictionary coverage (including **the two frontend-generated keys hardcoded in client.ts**), **every backend field name and validation rule has a message key** |
| `config/config_test.go` | defaults without a config file, partial file overrides, environment overriding the file, unknown fields rejected, invalid values rejected, the candidate order of `web.dir` |
| `httpserver/static_test.go` | SPA fallback, cache headers, API 404s returning JSON, startup failing on an incomplete directory |
| `httpserver/router_test.go` | **fail-closed assertions on the public endpoint set**, **detection of missing assembly**, 401 for everything unauthenticated, the full login/logout path, a disabled user losing access immediately, the response envelope, error keys, the field-level validation array, 413/404 mapping |
| `httpserver/rbac_api_test.go` | permission isolation (holding the code passes, missing it gives 403), grants taking effect per code, unknown permission codes rejected, the 409 impact and cascade confirmation, the four delete guards, unique conflicts carrying the field name, resetting a password kicking sessions |
| `middleware/middleware_test.go` | rate limiting per IP, spoofed `X-Forwarded-For` being ignored, the request body cap, cache headers matching the prefix only |
| `system/system_test.go` | parsing `/proc/stat` / `meminfo` / `statm` / `loadavg` (using real samples), `guest` not counted twice, units and the unavailable flag, non-Linux platforms reporting unavailability |
| `httpserver/monitor_test.go` | the session list and kicking, not being able to kick yourself, login logs recording success and origin, operation logs recording writes and 403s, **operation logs containing no request bodies** |
| `service/job_log_test.go` | every run (success / failure / skip) leaves a record with its trigger, **both writes share one transaction** (when the second fails, `last_run_at` does not move), logs are cleaned up by retention without touching fresh rows |
| `httpserver/job_logs_test.go` | the three authorization layers on run history (401 / 403 / `tool:job:list` allows), **one trigger makes exactly one row appear**, an unregistered key returns 404, an out-of-range page is clamped to the last one |
| `httpserver/fixture_test.go` | not a test case but the fixture the other tests reuse (a temporary database plus the full router) |
| `TestPanicReturnsEnvelope` in `httpserver/router_test.go` | **a panic still returns the envelope** — a 500 with an empty body makes the frontend mistake "the backend errored" for "the backend is not up" |

### ⚠️ Three cross-language cases need `-count=1`

One case each in `perm/perm_test.go`, `httpx/response_test.go` and `repository/menu_seed_test.go` reads frontend source
(the i18n dictionaries, and the page files under `web/src/pages/`) to confirm that "the keys the backend emits",
"the page a menu seed points at" and "the files the frontend really has" have not drifted apart.

**Go's test cache does not track files opened with `os.ReadFile` while a test runs**, and these three cases read outside
their package directory. Without `-count=1`, editing a frontend dictionary and re-running `go test` returns the stale
`ok` from the cache — measured:

```
$ go test ./internal/perm/        # after deleting a key from the dictionary
ok  mini-ruoyi/internal/perm  (cached)     ← a false pass
$ go test ./internal/perm/ -count=1
--- FAIL: TestFrontendDictCoversPermKeys
    缺少文案键 "perm.system.user.resetPwd"
```

CI has a cold cache and is unaffected; locally use `make test` or `make check`.

Two things to know when writing tests: **gin's validator is a global singleton**, so do not depend on registration order
outside `TestMain`; and every `newXxxFixture` in a test must run `repository.Migrate`, or the missing tables produce a pile
of 500s.

## 10. Adding a resource, end to end

This section is a "copy this" checklist. A plain resource touches **12 files** (8 backend, 3 frontend, 1 menu-seed
migration) plus 3 test files, and every step has something to copy from. **The reference is `role`** — the most standard
"single table + paged list + create/update/delete" module, with no uploads, passwords or trees to distract you.

| What to write | Copy from | Leave out |
| --- | --- | --- |
| Entity | `Role` in `domain/rbac.go` | new resources get their own `domain/<resource>.go` (only same-family entities share a file) |
| Table DDL | `sys_roles` in `migrations/0002_init_rbac.sql` | — |
| Repository | `repository/role_repository.go` | `Grants` / `ReplaceGrants` and the other grant-specific methods |
| Service | `service/role_service.go` | `Grants` / `SetGrants` |
| Handler | `handler/role_handler.go` | `Grants` / `SetGrants` (keep `cascadeRequested` — delete confirmation needs it) |
| Frontend page | `web/src/pages/system/roles.svelte` | the whole authorization tab |

Taking a new `widget` as the example:

1. **Entity** — `domain/widget.go`: fields plus JSON tags (the field names are the public contract, see §5.2)
2. **Table DDL** — `repository/migrations/0013_init_widgets.sql`: the highest existing number is `0012`, and migrations only ever append
3. **Repository** — `repository/widget_repository.go`: `List` / `Count` / `GetByID` / `Create` / `Update` / `Delete`,
   returning `domain.ErrNotFound` when absent, with an `ExistsXxx` pre-check for uniqueness
4. **Service** — `service/widget_service.go`: pagination normalized into `Page[T]`, uniqueness conflicts returned as `domain.Duplicate("field")`,
   and `Impact` computed before a delete (`domain.HasDependents` when something depends on it)
5. **Handler** — `handler/widget_handler.go`: bind + validate + call the service, handing errors to `httpx.FailBindError` / `FailFromError`.
   Validation rules live in the request struct's `binding` tag (`binding:"required,min=2,max=64"`), and
   `FailBindError` expands it into the field-level array — no hand-written error handling
6. **Permission codes** — `perm/perm.go`: declare `SystemWidgetList/Add/Edit/Delete` and add them to `Groups()`.
   Every code must belong to exactly one group, or the permission screen never shows it
7. **Routes** — `httpserver/router.go`: `reg.protect(http.MethodGet, "/widgets", perm.SystemWidgetList, deps.Widget.List)`.
   **The permission code is a required argument** — leaving it out does not compile; and do not register a business
   endpoint as public, because the public set is pinned by a test
8. **Wiring** — `cmd/server/main.go`: `NewWidgetRepository` → `NewWidgetService` → `NewWidgetHandler`, then into `deps`
9. **Copy** — `web/src/lib/i18n/zh-CN.ts` and `en-US.ts` each need three things: the permission-code text
   (derived backend-side as `system:widget:add → perm.system.widget.add`), the page text, and the menu title key `menu.tool.widget`
10. **Menu seed** — a new migration (copy `0009_seed_tool_menu.sql`): directories use `INSERT ... SELECT ... WHERE NOT EXISTS`
    to stay idempotent, children look their parent up by `title_key`. **Skip this and the page cannot be reached from the UI at all**
11. **Frontend page** — `web/src/pages/tool/widgets.svelte`: its `component` must match the seed from step 10
    (path relative to `src/pages`, no extension)
12. **Tests** — one each in `repository` (repository + seed), `service` (business rules, fixture `newJobFixture`) and
    `httpserver` (end to end plus the three authorization layers, fixture `testDeps`)

**Which omission gets caught by what**

| Forgot | What stops you |
| --- | --- |
| No permission code declared, or a route registered without one | compile error / startup panic (fail-closed) |
| Code not added to `Groups()` | `TestGroupsCoverAllCodesExactlyOnce` |
| Frontend missing the `perm.*` text | `TestFrontendDictCoversPermKeys` |
| Menu seed missing or misnamed | `TestMenuSeedResolvesToFrontend` (both directions: it also flags pages nobody can reach) |
| Table DDL out of sync with the migration records | schema verification at startup (`verifySchema`) |
| Resource not wired into `deps` | `TestRouterRejectsIncompleteDeps` |

Finish with `make check` (gofmt + vet + cross-compilation + all backend tests + frontend type check), then run
`make test-e2e` to actually click through the page.