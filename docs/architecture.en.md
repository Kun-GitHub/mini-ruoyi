# Overall architecture

English | [简体中文](architecture.md)

This document covers mini-ruoyi's system overview, the frontend/backend contract and the key decisions.
Layering details live in [architecture-server.en.md](architecture-server.en.md) and
[architecture-web.en.md](architecture-web.en.md).

## 1. System overview

There is exactly one process: no reverse proxy, no separate database service, no cache service.

```
                        ┌───────────────────────────────────────────────────┐
  browser               │  mini-ruoyi (one Go process)                      │
  ─────────────────────▶│                                                   │
    :8080               │                                                   │
                        │  Gin router                                       │
                        │   ├── /healthz      ──▶ SQLite ping               │
                        │   ├── /api/v1/*     ──▶ business logic            │
                        │   ├── /assets/*     ──▶ files on disk             │
                        │   └── anything else ──▶ index.html (SPA fallback) │
                        │                                                   │
                        │  SQLite (WAL)                                     │
                        └──────────────┬────────────────────────────────────┘
                                       ▼
                             data.db / data.db-wal
```

Deployment directory:

```
/opt/mini-ruoyi/
├── mini-ruoyi        # the binary (all backend logic inside)
├── web/              # frontend output: index.html + assets/
└── data.db           # SQLite database
```

The frontend output is **not** inside the binary. That is a deliberate trade-off; see §5 for why.

## 2. Build pipeline

```
web/src/**  ──[vite build]──▶  web/dist/  ──[cp -R]──▶  bin/web/
                                                            │
server/**   ──[go build]────────────────────▶  bin/mini-ruoyi
                                                            │
                                            bin/  ──[scp]──▶  server
```

`make build` does the build half of the first four steps in one command (build the frontend, copy its output, compile
the binary); the final `scp` is yours to wire into whatever release process you have. While developing, use
`make dev-server` + `make dev-web`: the Vite dev server proxies `/api` and `/healthz` to `:8080`, so frontend hot
reload and the backend stay out of each other's way.

## 3. Frontend/backend contract

### 3.1 Response envelope

**Every** JSON endpoint shares the same envelope, errors included:

```jsonc
{
  "code": 200,                     // always equals the HTTP status code
  "msg":  "ok",                    // "ok" on success; an i18n key on failure (see §6)
  "data": { },                     // present only on success
  "errors": [ ]                    // present only when field-level validation failed
}
```

**Semantics are carried by the HTTP status code**, and `code` is always that same status code (`write()` sets it; callers
never touch it). This is a deliberate departure from upstream RuoYi (which always returns HTTP 200 with
`code: 200/500`) — keeping HTTP semantics means curl, logs, monitoring and gateways can all read the error rate
directly, without parsing the body.

**Why `code` duplicates the status instead of being 0/1:** Go's `int` zero value is 0 and the `Code` field has no
`omitempty`, so using 0 for success means any new code path that **forgets to set `Code`** silently reports success.
Measured:

```go
json.Marshal(Response{Msg: "ok", Data: x})   // forgot to set Code
→ {"code":0,"msg":"ok",...}                // the frontend sees success
```

Using the status code makes the zero value 0 **impossible to be a legal value** — forgetting to set it fails loudly.

`code`'s only consumer is the **envelope discriminator** (see [architecture-web.en.md](architecture-web.en.md) §5.4):
the frontend uses it to tell "this is our response" from "a proxy answered instead". Success is decided by the HTTP
status, not by `code`.

| Case | HTTP | `msg` |
| --- | --- | --- |
| Success | 200 | `ok` |
| Parameter validation failed | 400 | `error.validationFailed` |
| Malformed request body | 400 | `error.malformedBody` |
| Invalid path parameter | 400 | `error.invalidId` |
| Malformed date filter | 400 | `error.invalidDate` |
| Invalid parent node | 400 | `error.invalidParent` |
| Resource not found | 404 | `error.notFound` |
| Not logged in / session expired | 401 | `error.unauthorized` |
| Wrong username or password | 401 | `error.badCredentials` |
| Built-in resource cannot be changed/deleted | 403 | `error.protected` |
| Account disabled | 403 | `error.accountDisabled` |
| Logged in but missing a permission | 403 | `error.forbidden` |
| CSRF token missing or mismatched | 403 | `error.csrfInvalid` |
| Cannot delete the account you are logged in as | 403 | `error.cannotDeleteSelf` |
| Dependent data exists, confirmation needed | 409 | `error.hasDependents` (the body carries the impact; see §3.4) |
| Cannot delete/disable the last administrator | 409 | `error.lastAdmin` |
| Unique field conflict | 409 | `error.duplicate` (`errors[]` carries the conflicting field name) |
| An unknown permission code was submitted | 400 | `error.invalidPermCode` |
| The response is not this service's envelope | any | `error.backendUnreachable` (**produced by the frontend**: the request never reached the backend; see [architecture-web.en.md](architecture-web.en.md) §5.4) |
| Request body too large | 413 | `error.bodyTooLarge` |
| Rate limit hit | 429 | `error.tooManyRequests` |
| Service unavailable | 503 | `error.serviceUnavailable` |
| Internal server error | 500 | `error.internal` |
| Wrong current password | 400 | `error.wrongOldPassword` |
| The uploaded file itself is invalid (empty, say) | 400 | `error.invalidFile` |
| The cron expression cannot be parsed | 400 | `error.invalidJobCron` |
| Cannot kick your own current session | 403 | `error.cannotKickSelf` |
| A single file exceeds the limit | 413 | `error.fileTooLarge` |
| Total capacity quota exceeded | 413 | `error.quotaExceeded` |

The complete key list is in §6 (including the two the frontend alone produces).

### 3.2 Field-level validation failure

The backend only reports "which field, which rule, what the rule's parameter was" and **never the message text**:

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

- `field` is the **JSON field name** (`name`), not the Go field name (`Name`)
- `param` is omitted when the rule takes no parameter
- The message text is assembled by the frontend from `rule` + `param` + the field label; see
  [architecture-web.en.md](architecture-web.en.md)

### 3.3 Contract invariants

Any new endpoint has to preserve these:

1. The response body always has `code` and `msg`, and `code` always equals the HTTP status code (enforced by `httpx.write()`)
2. A failing response's `msg` is always an i18n key and **never contains natural language** (otherwise the frontend cannot translate it)
3. `data.list` is `[]` rather than `null` for an empty result (otherwise the frontend has to special-case it)
4. List endpoints use the fixed pagination shape `{list, total, page, page_size}`, where:
   - a `page_size` below 1 becomes the default 20, and one **above the cap of 100 is clamped to 100** (not quietly replaced
     with 20 — asking for 101 and getting 20 makes the caller believe there are only 20 rows)
   - the `page` in the response is the page **actually used**, already clamped to the last page when out of range. The
     frontend trusts it; otherwise a stale bookmark makes the pager read "99 / 2" over an empty table
5. Time fields are emitted as RFC 3339 UTC (Go's default `time.Time` JSON encoding)

### 3.4 Deleting a resource that has dependents

A delete cannot be executed unconditionally when the resource has children. The contract is: **the server computes the
impact right there at delete time, and when there are dependents it returns `409` with that impact; the frontend turns it
into a dialog, and after confirmation the request is re-sent with `?cascade=true`.**

```
DELETE /api/v1/menus/5
→ 409 Conflict
  {"code":409,"msg":"error.hasDependents",
   "data":{"child_menus":3,"affected_roles":2}}

DELETE /api/v1/menus/5?cascade=true
→ 200 {"code":200,"msg":"ok","data":{"id":5}}
```

The impact each resource reports:

| Resource | Impact fields | Meaning |
| --- | --- | --- |
| Menu | `child_menus` | Number of descendant menus (a cascade delete removes them too) |
| Menu | `affected_roles` | Number of roles referencing this menu or any of its descendants |
| Role | `affected_users` | Number of users holding this role |
| User | — | Not guarded. Role assignments are the user's own subordinate data, so losing them on delete is expected |

**Why "409 + re-send" instead of a separate pre-flight endpoint:**

- The check and the delete happen in the same request, so there is no TOCTOU window. With a
  `GET .../deletion-impact` pre-flight, another administrator adding a child between the dialog and the confirmation
  would have the user delete more than they knew about — and "don't let the user delete more than they expected" is the
  whole point of this feature
- With no dependents (the vast majority of cases) it costs a single round trip

**One rule the frontend must follow:** `409` with `msg == "error.hasDependents"` **is not an error**. Do not show an
error toast — show the confirmation dialog and the numbers from `data`.

New error key: `error.hasDependents` (keep `web/src/lib/i18n/` in sync).

## 4. Cache policy

```
/index.html      Cache-Control: no-cache                        revalidated on every request
/assets/*        Cache-Control: public, max-age=31536000, immutable   content-hashed, cached forever
/api/*           not cached
```

`index.html` **must** be `no-cache`. Vite's asset filenames carry a content hash while `index.html` does not; if
`index.html` were cached, a user's refresh would keep referencing a hash that no longer exists and the frontend could
never update itself. This is the precondition that makes "rebuild the frontend, refresh, and you are on the latest
version" true.

## 5. Key decisions and trade-offs

| Decision | Alternative | Why this one |
| --- | --- | --- |
| **One process, static assets served from disk** | A separate static server / nginx in front | On 1C1G it saves a long-running process and a config file; on a single machine nginx only adds operational cost |
| **Frontend output is not embedded in the binary** | `go:embed` into a single file | The frontend updates independently, with no backend restart (with `go:embed` every frontend change means recompiling and restarting). The price is that the deployment unit goes from one file to one directory, and the binary is about 300 KB smaller |
| **SQLite as the only data source** | Support MySQL + SQLite at once | Two data sources means two sets of DDL, two SQL dialects, a doubled test matrix and MySQL in CI. Under a "minimal" goal that cost buys nothing |
| **`modernc.org/sqlite`** | `mattn/go-sqlite3` (CGO) | Pure Go, so `GOOS/GOARCH` cross-compilation needs no toolchain. Roughly half the performance of the CGO build — entirely sufficient for a single-machine admin panel |
| **No ORM** | GORM / Ent / sqlc | An admin panel runs single-digit QPS, so an ORM's reflection and code generation earn little; hand-written SQL is easier to debug and fits "minimal" |
| **HTTP semantic status codes** | RuoYi-style always-200 + `code: 200/500` | See §3.1. `code` duplicates the status rather than being 0/1: Go's `int` zero value is 0, so using 0 for success makes a forgotten `Code` assignment silently mean success |
| **The backend emits only i18n keys** | The backend emits text based on `Accept-Language` | The text only has to exist in one place (the frontend, where the UI lives); switching language needs no new request; and the backend stays language-neutral, so users of any locale can use it |
| **Hand-written frontend i18n** | svelte-i18n / paraglide-js | Two locales and a small message count make a runtime library's dynamic loading and formatting plugins pointless; the hand-written version uses TypeScript types to make a missing key a compile error |
| **Versioned migrations** | `CREATE TABLE IF NOT EXISTS` | The latter cannot alter a table, which amounts to "hand-edit the database after release" |
| **Cookie + server-side session for auth** | Stateless JWT | On one machine and one process, JWT's horizontal-scaling advantage is zero while every drawback — no active logout, no session kick, permission changes not taking effect immediately — applies. RuoYi's own JWT is a shell anyway, with the real state in Redis; we have no Redis, and a session table is semantically equivalent. Implementation in [schema.en.md](schema.en.md) §3.7 |
| **No Redis** | Redis for sessions | On a single machine Redis adds one more deployment unit and one more failure point. A local SQLite primary-key lookup (~1-3 µs) is faster than Redis over loopback (~30-60 µs) |
| **Content-hashed assets cached forever** | `no-cache` everywhere | Removes every repeat request after the first paint; the hash guarantees the wrong version is never served |

## 5.1 The four deployment shapes

The frontend output is plain static files that anything can serve. **The backend falls back to API-only mode when the
`web/` directory is absent**, so all four of these work:

| Shape | Frontend served by | Configuration needed |
| --- | --- | --- |
| 1. Single binary | Go (`bin/web/`) | none; the default |
| 2. Local development | Vite dev server (`/api` proxied to the backend) | point `APP_WEB_DIR` at a non-existent location (or simply do not build the frontend) |
| 3. nginx serves the frontend | an nginx static root + `try_files` | `APP_TRUSTED_PROXIES=<proxy address>` |
| 4. nginx terminates TLS only | Go (`bin/web/`) | `APP_TRUSTED_PROXIES` + `APP_SECURE_COOKIE=true` |

**Shape 2** (Vite development):

```bash
make dev-server   # backend on :8080; with no bin/web it prints a hint and serves the API as usual
make dev-web      # Vite on :5173, proxying /api and /healthz to :8080
```

Opening `http://localhost:8080/` at this point shows
`{"code":404,"msg":"error.frontendDisabled"}` — **that is expected**: it states plainly that "the backend is in API-only
mode and no frontend is deployed", rather than leaving you to think the service is broken.

**Shape 3** (nginx serves the frontend):

```nginx
server {
    listen 80;
    root /opt/mini-ruoyi/web;

    # content-hashed assets can be cached forever (same policy as when Go serves them)
    location /assets/ {
        add_header Cache-Control "public, max-age=31536000, immutable";
        try_files $uri =404;
    }

    # index.html must be revalidated, or a user's refresh cannot pick up a rebuilt frontend
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

On the backend side:

```bash
APP_TRUSTED_PROXIES=127.0.0.1   # required; see below
```

### ⚠️ Behind a reverse proxy, `APP_TRUSTED_PROXIES` is mandatory

Its default is **empty**, meaning no `X-Forwarded-For` is trusted. That default is correct: this service can listen
publicly on its own, and trusting the header unconditionally would let any client claim any IP, making per-IP rate
limiting meaningless.

The flip side: behind nginx you **must** declare the proxy address, or every request appears to come from `127.0.0.1`:

| Configuration | What `login_ip` records | The real effect of the per-IP rate limit |
| --- | --- | --- |
| unset | `127.0.0.1` (the proxy) | **Degenerates into a global limit**: one person hitting 20 rps gets everyone a 429 |
| `APP_TRUSTED_PROXIES=127.0.0.1` | the real client IP | each IP is counted on its own |

IPs and CIDRs are both accepted, comma-separated: `APP_TRUSTED_PROXIES=127.0.0.1,10.0.0.0/8`.
An invalid value **fails at startup** with the problem named, never silently ignoring it.

> Note: even with trusted proxies configured, the **leftmost** value of `X-Forwarded-For` may be forged by the client.
> This service uses gin's default strategy (take the rightmost non-trusted address), which is safe as long as you only
> trust your own proxy.

## 5.2 Why audit logs are not written per request

`sys_login_logs` and `sys_oper_logs` are **not** written to the database directly from the middleware; they go through
"an in-memory channel buffer plus batch writes" (`service.LogService`):

```
request → RecordLogin/RecordOper → channel (capacity 1024)
                                    ↓
                      once 64 records accumulate or every 2 seconds → one transaction, batch insert
```

The reason: **SQLite has a single writer**. Writing a log row per request means contending for the write lock and paying
a transaction per request, so every request queues on that lock. Batching pays the cost once for N requests.

Three decisions come with it:

| Decision | Reason |
| --- | --- |
| A full buffer **drops logs** and counts them | This is an audit feature and must not slow the main path; the number of dropped records is logged at shutdown |
| `LogService.Stop()` at shutdown | Skip the flush and the last few seconds of logs are lost — precisely the window in which things go wrong |
| Retention deletes in batches (2000 rows at a time, checked hourly) | There may be a lot of expired rows, and one large DELETE holds the write lock long enough to queue every request |

**The price is up to 2 seconds of log lag.** The UI says so, and tests have to respect it: asserting "a record shows up
in the log" requires a reload plus retry, because Playwright's locator retry only re-evaluates the DOM and does not
re-issue the request.

Retention is controlled by `APP_LOG_RETENTION_DAYS`, 30 days by default.

**Job execution logs (`sys_job_logs`) take the other road: they are written synchronously**, in the same transaction as
`sys_jobs.last_*` (see [schema.en.md](schema.en.md) §3.11). A job runs a few times a day, so batching buys nothing —
and "the list says this run succeeded while the history has no such row" is not an acceptable inconsistency. Run
history therefore has no 2-second lag: a refresh shows it immediately.

## 5.3 Three security constraints on uploads

| Constraint | Reason |
| --- | --- |
| The on-disk name is generated server-side (random hex) and **the user-supplied filename is never used** | Using user input as a path is path traversal (`../../etc/passwd`) |
| The upload directory **must not be mounted statically**; it goes through authorization in a handler and `http.ServeContent` | A static mount bypasses authorization, so anyone who guesses a URL gets someone else's upload |
| Downloads always carry `Content-Disposition: attachment` + `nosniff` + a fixed `octet-stream` | Uploading an `.html` and having it render inline means running someone else's script on your own origin |

`http.ServeContent` rather than reading the file and writing it out by hand: it streams in chunks (reading a whole file
into memory on a 1 GB machine blows up) and gives Range support for free.

**Ordering for consistency**: an upload writes the file first and the row second; a delete removes the row first and the
file second. Both choices point the same way — **an orphan file is preferable to a row pointing at nothing**. Orphans are
reclaimed periodically by the `cleanup:orphan_files` job.

## 5.4 Why scheduled jobs cannot live in the database

RuoYi stores a cron expression in the database and reflectively invokes a bean method. That does not work in Go: there is
no safe way to reflectively call an arbitrary function.

So jobs are registered in code (`internal/job`) and the database only stores the on/off switch and the cron expression.
The UI offers no "new job" button — a job created there would never run.

The database does also hold **execution results** (the latest in `sys_jobs.last_*`, plus one row per run in
`sys_job_logs`). Those are two different things: *what a job is* can only live in code, while *how it ran* is data.

At startup the registry is upserted (`ON CONFLICT DO NOTHING`, so user-edited values are not overwritten), and keys that
exist in the database but not in the registry only produce a warning rather than refusing to start. **This is the
opposite of how permission codes are treated**: a job row that never runs is harmless, whereas an invalid permission code
silently breaks authorization.

Time zones: cron expressions are parsed in the **server's local time zone** ("3 a.m. every day" is the operator's mental
model), while recorded execution times are still UTC.

## 5.5 Why configuration is two layers ("file + environment variables")

Precedence is **environment variables > config file > built-in defaults**, and **it runs with no config file at all**.

| Decision | Reason |
| --- | --- |
| Keep environment variables, and give them the highest precedence | systemd and containers can only pass environment variables. Letting them override a single value avoids editing a (possibly read-only) config file |
| No config file is not an error | "Clone and run" is the floor for an open-source project. The defaults are a working single-machine configuration |
| **An unknown config field is a hard error** | `KnownFields(true)`. Otherwise writing `max_size` instead of `max_mb` produces no feedback and the user is left thinking "I configured it, why is it not taking effect?" — and that class of problem is very hard to trace back to configuration |
| **An invalid environment variable value is a hard error** | Same reasoning. Ignored silently, `APP_UPLOAD_MAX_MB=二十` quietly falls back to the default |
| Validate semantic combinations at startup | `quota_mb < max_mb` starts fine but cannot accept a single file; better to refuse at startup |
| Print a one-line summary at startup | Paths are always absolute. "Where is the data" and "where do uploads go" are the two questions operators ask most, and `data.db` / `uploads` in the config are relative to the working directory |

`config.yaml` is in `.gitignore` (it carries per-deployment details such as the listen address and proxy addresses); the
committable sample is `server/config/config.example.yaml`.

## 5.6 Why runtime metrics pull in no third-party library

The monitoring page reads CPU / memory / disk / process data straight from `/proc` and `Statfs` using the standard
library only — no `gopsutil` or the like. Why:

- About ten fields are needed in total, one parsed line each from `/proc/stat`, `/proc/meminfo` and `/proc/self/statm`
- Such libraries carry a pile of platform implementations for portability, while this project targets Linux

**The price is that CPU and memory are unreadable on macOS and Windows** (there is no `/proc`). The handling is that every
metric carries an `available` flag and returns `available: false` when it cannot be read, so the UI can say plainly that
the platform does not expose that metric.

That is deliberate: **returning 0 would be read as "load is very low", which is worse than nothing**.

Disk is the only part that has to be split per platform: `syscall.Statfs` only exists on Unix, and Windows goes through
kernel32's `GetDiskFreeSpaceExW` (standard library as well, no new dependency).
See `internal/system/disk_unix.go` and `disk_windows.go`.

> Platform-specific code compiling **on this machine does not mean** it compiles elsewhere.
> `make check-cross` compiles for five platforms precisely to catch that class of mistake.

A few implementation details:

| Detail | Why |
| --- | --- |
| CPU usage from the delta of two `/proc/stat` reads (200 ms apart) | Cumulative counters only give "the average since boot", which is useless for diagnosing the present |
| Memory from `MemAvailable`, not `MemFree` | `MemFree` excludes reclaimable page cache, so reading it suggests memory is nearly full when it is not |
| Process RSS from `/proc/self/statm`, not `syscall.Getrusage` | The latter's `Maxrss` is KB on Linux and bytes on macOS; one field in two units is a bug waiting to happen |
| Only the first 8 fields of `/proc/stat` | The kernel already counts `guest`/`guest_nice` into `user`/`nice`, so adding everything double-counts and inflates the utilization |
| Only lines carrying a `kB` unit in `/proc/meminfo` | Ignoring the unit reads `1000 MB` as `1000 kB` — off by 1000× |
| A missing `MemAvailable` reports "unavailable" rather than 0 | Treating it as 0 computes "100% used", a false alarm |
| Disk utilization from `total - Bfree`, while the UI's "available" shows `Bavail` | `Bavail` is what a non-root user can actually write (ext4 reserves 5% for root by default), so this matches `df`; the cost is that the three numbers do not add up to `total` |

These parsing paths only run on Linux, while development machines are usually macOS, so `internal/system/system_test.go`
uses **real `/proc` content** as samples — "it compiles on Linux" verifies nothing about whether the parsing is right.

## 6. Error key list

The backend defines them in `server/internal/httpx/response.go`; the frontend dictionary is
`web/src/lib/i18n/zh-CN.ts`. **The two must stay in sync** — a new error key means editing both.

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
error.wrongOldPassword      error.invalidDate
# the next two are produced by the frontend only (web/src/lib/api/client.ts); the backend never returns them:
error.network               # fetch threw: nothing answered at all
error.backendUnreachable    # something answered but it was not the envelope: a proxy intercepted it, or the backend is not running
```

Apart from the last two, these keys are defined in `server/internal/httpx/response.go`, with the text in the frontend
dictionary. The two sides are coupled by string convention, so a missing entry does not raise an error — the UI just
shows the raw key name, `error.notFound`, to users. Two tests guard this:

| Test | Scans |
| --- | --- |
| `TestFrontendDictCoversErrorKeys` | the key constants in the backend's `response.go` |
| `TestFrontendGeneratedErrorKeysAreTranslated` | the key literals hardcoded in the frontend's `client.ts` |

Both are needed. `error.backendUnreachable` also has a constant on the backend (it is what forces the frontend
dictionary to register it; the backend itself never returns it), so the first test covers it. But **`error.network`
has no constant anywhere on the backend**, so only the second test can catch it.

## 7. Design trade-offs under 1C1G

| Measure | Where | Effect |
| --- | --- | --- |
| Per-IP rate limit of 20 rps / burst 40 | `middleware.RateLimit` | Keeps a burst from crushing the process; **per IP rather than process-wide**, or one client exhausting the quota would turn every user's request into a 429 |
| Lazy GC of the limiter (3 minutes) | same | Without it the map grows unbounded with every IP ever seen while being scanned, which is more dangerous than the limiting itself |
| 1 MiB request body cap | `middleware.BodyLimit` | A single oversized body can blow up 1 GB of memory |
| SQLite connection pool capped at 4 | `repository.NewDB` | SQLite has one writer; extra connections only add memory and lock waiting |
| `busy_timeout=5000` | DSN `_pragma` | On a write conflict, wait instead of failing immediately with `SQLITE_BUSY` |
| WAL + `synchronous=NORMAL` | same | Reads and writes do not block each other, and it is much faster than `FULL` |
| `GOMEMLIMIT=700MiB` | systemd environment | Caps the Go heap soft limit to stay clear of the OOM killer |

## 8. To-do list

Authentication, authorization, dynamic routing and audit logging are all in place (the
implementation notes are in §5 above and in [architecture-server.en.md](architecture-server.en.md)); only the following
remains.

> One thing that is deliberately **not** done: authorization has **no cross-request permission cache**.
> `Auth.Require` calls `IdentityOf` on every request (reading `sys_user_roles` + `sys_role_perms`), and
> `RequirePerm` reuses the copy resolved within that same request. That is intentional — see the reasoning in §5 for
> rejecting JWT: caching permissions turns "I changed a role but the user still has the old permissions" into a
> phenomenon you can only explain by waiting for a cache to expire. On a single-machine SQLite setup that query cost is
> far below the certainty it buys.

| Item | Notes |
| --- | --- |
| A reproducible initial database | `server/data.db` currently ships in the repository. It doubles as the live development database, so a single login writes sessions and logs into it — run `make db-clean` before committing. The end state is to regenerate it from `migrations/` + seed SQL and stop shipping a database at all |
| Font size | The Inter variable font carries every subset, for 213 KB of woff2 in `dist`. Restricted to Chinese and English it could be trimmed to latin + latin-ext |

Frontend-side to-dos are in [architecture-web.en.md](architecture-web.en.md) §9, backend-side ones under "Not implemented
yet" in [../server/README.en.md](../server/README.en.md), and schema-side ones in
[schema.en.md](schema.en.md) §11.