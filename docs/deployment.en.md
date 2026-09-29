# Deployment and operations

English | [简体中文](deployment.md)

Aimed at the self-hosted 1-core, 1 GB case. For the architecture, see [architecture.en.md](architecture.en.md);
for the full list of settings, see [../server/README.en.md](../server/README.en.md).

## 1. Deployment unit

```
/opt/mini-ruoyi/
├── mini-ruoyi          # the binary
├── web/                # frontend build output (must sit next to the binary; the backend looks there)
├── config/config.yaml  # configuration (optional; it runs without one)
├── data.db             # SQLite database
├── data.db-wal         # WAL, created automatically at runtime
└── uploads/            # the uploaded files themselves
```

**The application's entire state is `data.db` and `uploads/`** — those two are all that backup and migration have to deal with.

## 2. systemd

`deploy/mini-ruoyi.service` can be used as-is:

```bash
sudo useradd --system --home /opt/mini-ruoyi --shell /usr/sbin/nologin mini-ruoyi
sudo cp bin/mini-ruoyi /opt/mini-ruoyi/
sudo cp -r bin/web      /opt/mini-ruoyi/
sudo chown -R mini-ruoyi:mini-ruoyi /opt/mini-ruoyi
sudo cp deploy/mini-ruoyi.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now mini-ruoyi
```

Three things the unit already sets, and **must not be removed**:

| Setting | Why |
| --- | --- |
| `GOMEMLIMIT=700MiB` | Without it the GC climbs to the machine's memory limit and the OOM killer takes the process down; the symptom is "the process randomly restarts" |
| `GOGC=50` | Collect earlier, trading a little CPU for a lower memory peak |
| `GOMAXPROCS=1` | Extra Ps buy nothing on a single-core machine |

`ProtectSystem=strict` + `ReadWritePaths=/opt/mini-ruoyi` restrict the process to writing inside its own directory.
If `data.db` lives somewhere else (a separate data disk, say), remember to add that path to `ReadWritePaths`.

## 3. nginx (optional)

`deploy/nginx.conf.example` is a complete example. **The backend can listen directly on the internet without nginx**;
look at it when you need TLS, or when you want nginx to serve the frontend.

⚠️ Use nginx and you need to know these two:

```bash
APP_TRUSTED_PROXIES=127.0.0.1   # required
APP_SECURE_COOKIE=true          # required whenever a TLS terminator is in front
```

What happens without `APP_TRUSTED_PROXIES`: every request appears to come from `127.0.0.1`, so
**the per-IP rate limit degrades into a global one** — one person hitting 20 rps gets everyone a 429;
the IPs in the login log are all the proxy address too, which makes the audit trail worthless.

## 4. Backup

**The state is `data.db` and `uploads/`**, and only backing up both is complete.

### 4.1 The database

⚠️ **Never `cp data.db` while the service is running.** With WAL enabled the most recent commits may still be in
`data.db-wal`, so copying the main file alone loses data; copying all three files still races.

Two reliable approaches:

```bash
# Preferred: SQLite's own backup command, which takes a consistent snapshot while running
sqlite3 /opt/mini-ruoyi/data.db ".backup '/backup/data-$(date +%F).db'"
# Equivalent form (needs SQLite 3.27+)
sqlite3 /opt/mini-ruoyi/data.db "VACUUM INTO '/backup/data-$(date +%F).db'"
```

```bash
# Or: stop the service and copy, which is the cleanest of all
systemctl stop mini-ruoyi
cp /opt/mini-ruoyi/data.db* /backup/
systemctl start mini-ruoyi
```

### 4.2 Uploaded files

```bash
rsync -a --delete /opt/mini-ruoyi/uploads/ /backup/uploads/
```

`uploads/` is just ordinary files, so copying it while running is safe (a file that is mid-upload gets cleaned up by
the `cleanup:orphan_files` job on its next run).

### 4.3 A full backup script

```bash
#!/bin/sh
set -eu
STAMP=$(date +%F-%H%M)
DEST=/backup/$STAMP
mkdir -p "$DEST"
sqlite3 /opt/mini-ruoyi/data.db ".backup '$DEST/data.db'"
rsync -a --delete /opt/mini-ruoyi/uploads/ "$DEST/uploads/"
# keep only the 14 most recent
ls -1dt /backup/*/ | tail -n +15 | xargs -r rm -rf
```

### 4.4 Restore

```bash
systemctl stop mini-ruoyi
rm -f /opt/mini-ruoyi/data.db-wal /opt/mini-ruoyi/data.db-shm   # must be removed, or they mix with the old data
cp /backup/2025-09-28-0300/data.db /opt/mini-ruoyi/data.db
rsync -a --delete /backup/2025-09-28-0300/uploads/ /opt/mini-ruoyi/uploads/
chown -R mini-ruoyi:mini-ruoyi /opt/mini-ruoyi
systemctl start mini-ruoyi
```

Removing the `-wal` / `-shm` files matters: they **belong to the database they were written with**, and leaving an old
database's WAL behind would combine with the restored file into a state nobody has ever seen.

## 5. Upgrade

```bash
systemctl stop mini-ruoyi
cp /opt/mini-ruoyi/mini-ruoyi /opt/mini-ruoyi/mini-ruoyi.bak   # keep something to roll back to
cp bin/mini-ruoyi /opt/mini-ruoyi/
rsync -a --delete bin/web/ /opt/mini-ruoyi/web/
systemctl start mini-ruoyi
journalctl -u mini-ruoyi -n 30
```

Migrations that have not been applied run automatically at startup. **Read the log before walking away** — a failed
migration makes the process exit outright (deliberately: running on a half-migrated schema shows up much later as
something that looks unrelated).

### Frontend-only update

The frontend output is not inside the binary, so changing the frontend **needs no backend restart**:

```bash
rsync -a --delete bin/web/ /opt/mini-ruoyi/web/
```

Users get the new version on refresh (`index.html` is `no-cache` and `assets/*` are content-hashed).

> Exception: if a user **has the page open right now** and you delete the old hashed file it references between two
> refreshes, that page's lazy import 404s. The frontend listens for `vite:preloadError` and reloads the whole page
> Exception: if a user **has the page open right now** and you delete the old hashed file it references between two
> refreshes, that page's lazy import 404s. The frontend listens for `vite:preloadError` and reloads the whole page
> automatically, so it shows up as "I clicked something and the page refreshed itself" rather than a blank screen.

> ⚠️ **Frontend-only updates only work while the envelope contract is unchanged.** After a change to the response
> envelope (the value of `code`), an old frontend rejects every response from the new backend — every request reports
> **"Cannot reach the backend service"**, login included.
>
> You must **update the frontend and get the browser to refresh at the same time**: `index.html` is `no-cache`, so one
> manual refresh is enough. If `index.html` itself is cached (a CDN in front, say), purge it. When upgrading the backend
> binary, `rsync` the `web/` directory along with it — that is the simplest safe habit.

## 6. Troubleshooting

| Symptom | Look at first |
| --- | --- |
| Opening `:8080` shows `{"code":404,"msg":"error.frontendDisabled"}` | The backend is in API-only mode and did not find the `web/` directory. Look for the "前端目录 ... 不存在" line in the startup log |
| The frontend says it cannot reach the backend | The backend is not running, or the proxy is misconfigured. The browser console prints the actual HTTP status and response body |
| An endpoint returns 500 | `journalctl -u mini-ruoyi`. Every 500 goes into the log in full; the client only gets a key |
| Everyone gets a 429 at once | There is a proxy in front but `APP_TRUSTED_PROXIES` is unset, so the rate limit went global |
| The process randomly restarts | `dmesg \| grep -i oom`. Usually a missing `GOMEMLIMIT` |
| The monitoring page shows the CPU / memory metrics as unsupported | Expected. Those two are read from `/proc` and only exist on Linux. Disk and process metrics work on every platform |
| You are logged out immediately after logging in | `APP_SECURE_COOKIE=true` while browsing over HTTP, so the browser never sends the cookie back |

Runtime state is visible on the **System monitoring** page (CPU / memory / disk / Go process), and scheduled job state
is under **System tools → Scheduled jobs**, including "did the last run succeed" and when the next one is due. The
**execution history** button on a job shows every run's trigger, result and duration.