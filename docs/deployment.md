# 部署与运维

[English](deployment.en.md) | 简体中文

面向 1 核 1G 的自托管场景。架构说明见 [architecture.md](architecture.md)，
配置项清单见 [../server/README.md](../server/README.md)。

## 1. 部署单元

```
/opt/mini-ruoyi/
├── mini-ruoyi          # 二进制
├── web/                # 前端产物（必须与二进制同级，后端按此规则查找）
├── config/config.yaml  # 配置（可选，不改也能跑）
├── data.db             # SQLite 数据库
├── data.db-wal         # WAL，运行时自动生成
└── uploads/            # 上传的文件本体
```

**应用的全部状态就是 `data.db` 和 `uploads/` 两样**，备份与迁移只需处理它们。

## 2. systemd

`deploy/mini-ruoyi.service` 可直接用：

```bash
sudo useradd --system --home /opt/mini-ruoyi --shell /usr/sbin/nologin mini-ruoyi
sudo cp bin/mini-ruoyi /opt/mini-ruoyi/
sudo cp -r bin/web      /opt/mini-ruoyi/
sudo chmod +x /opt/mini-ruoyi/mini-ruoyi   # 在 Windows 上交叉编译出来的二进制只有 0644，不补这一刀 systemd 会以 203/EXEC 失败
sudo chown -R mini-ruoyi:mini-ruoyi /opt/mini-ruoyi
sudo cp deploy/mini-ruoyi.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now mini-ruoyi
```

还没上 systemd 时，直接在部署目录里前台跑一遍（第一次上线排查用）：

```bash
cd /opt/mini-ruoyi && ./mini-ruoyi          # 默认 :8080，Ctrl-C 停止
APP_ADDR=127.0.0.1:8080 ./mini-ruoyi        # 环境变量覆盖配置（优先级：默认值 → config.yaml → 环境变量）
```

首次启动会建 `data.db` 并跑完所有迁移；日志里 `监听 :8080（prod）| 数据库 … | 前端 …` 那行确认
监听地址、数据库、前端目录都指着预期位置。工作目录必须是 `/opt/mini-ruoyi`，否则默认配置路径
`config/config.yaml` 和 `data.db` 会落到别处（用 `APP_CONFIG` 与 `APP_DB_PATH` 也可显式指定）。

unit 里已经设好的三件事，**不要删**：

| 配置 | 为什么 |
| --- | --- |
| `GOMEMLIMIT=700MiB` | 不设的话 GC 会一路吃到机器内存上限，最后被 OOM Killer 干掉，症状是「进程莫名重启」 |
| `GOGC=50` | 更早回收，用一点 CPU 换内存峰值 |
| `GOMAXPROCS=1` | 单核机器上多开 P 没有收益 |

`ProtectSystem=strict` + `ReadWritePaths=/opt/mini-ruoyi` 让进程只能写自己的目录。
如果 `data.db` 放在别处（比如单独的数据盘），记得把它加进 `ReadWritePaths`。

## 3. nginx（可选）

`deploy/nginx.conf.example` 是完整示例。**后端可以直接对外监听，不用 nginx**；
需要 TLS 或想让 nginx 托管前端时再看它。

⚠️ 用 nginx 就必须知道这两条：

```bash
APP_TRUSTED_PROXIES=127.0.0.1   # 必须
APP_SECURE_COOKIE=true          # 有 TLS 终止层时必须
```

不设 `APP_TRUSTED_PROXIES` 的后果：所有请求的来源 IP 都变成 `127.0.0.1`，
**按 IP 限流退化成全局限流**——一个人刷满 20 rps，所有人一起收到 429；
登录日志里的 IP 也全是代理地址，审计价值归零。

## 4. 备份

**状态只有 `data.db` 和 `uploads/`**，两样一起备份才完整。

### 4.1 数据库

⚠️ **运行中不要直接 `cp data.db`**。开了 WAL 之后，最近的提交可能还在
`data.db-wal` 里，单独拷主文件会丢数据；三件套一起拷也仍然有竞态。

两种可靠做法：

```bash
# 推荐：SQLite 自带的备份命令，运行中也能拿到一致快照
sqlite3 /opt/mini-ruoyi/data.db ".backup '/backup/data-$(date +%F).db'"
# 等价写法（需要 SQLite 3.27+）
sqlite3 /opt/mini-ruoyi/data.db "VACUUM INTO '/backup/data-$(date +%F).db'"
```

```bash
# 或者：停服务再复制，这时最干净
systemctl stop mini-ruoyi
cp /opt/mini-ruoyi/data.db* /backup/
systemctl start mini-ruoyi
```

### 4.2 上传的文件

```bash
rsync -a --delete /opt/mini-ruoyi/uploads/ /backup/uploads/
```

`uploads/` 只是普通文件，运行中拷贝是安全的（正在上传的那个文件会由定时任务
`cleanup:orphan_files` 在下次运行时收掉）。

### 4.3 完整备份脚本

```bash
#!/bin/sh
set -eu
STAMP=$(date +%F-%H%M)
DEST=/backup/$STAMP
mkdir -p "$DEST"
sqlite3 /opt/mini-ruoyi/data.db ".backup '$DEST/data.db'"
rsync -a --delete /opt/mini-ruoyi/uploads/ "$DEST/uploads/"
# 只保留最近 14 份
ls -1dt /backup/*/ | tail -n +15 | xargs -r rm -rf
```

### 4.4 恢复

```bash
systemctl stop mini-ruoyi
rm -f /opt/mini-ruoyi/data.db-wal /opt/mini-ruoyi/data.db-shm   # 必须删，否则会和旧数据混在一起
cp /backup/2025-09-28-0300/data.db /opt/mini-ruoyi/data.db
rsync -a --delete /backup/2025-09-28-0300/uploads/ /opt/mini-ruoyi/uploads/
chown -R mini-ruoyi:mini-ruoyi /opt/mini-ruoyi
systemctl start mini-ruoyi
```

删 `-wal` / `-shm` 这步很关键：它们是**跟着主库走的**，留着旧库的 WAL
会和恢复回来的主库拼出一份谁也没见过的状态。

## 5. 升级

```bash
systemctl stop mini-ruoyi
cp /opt/mini-ruoyi/mini-ruoyi /opt/mini-ruoyi/mini-ruoyi.bak   # 留一份能回滚
cp bin/mini-ruoyi /opt/mini-ruoyi/
rsync -a --delete bin/web/ /opt/mini-ruoyi/web/
systemctl start mini-ruoyi
journalctl -u mini-ruoyi -n 30
```

启动时会自动跑未应用的迁移。**先看日志再走人**——迁移失败会让进程直接退出
（这是有意的：带着半个数据结构跑起来，问题会在很久之后以无关的形式出现）。

### 只更新前端

前端产物不在二进制里，所以改前端**不需要重启后端**：

```bash
rsync -a --delete bin/web/ /opt/mini-ruoyi/web/
```

用户在浏览器里刷新即可拿到新版本（`index.html` 是 `no-cache`，`assets/*` 带内容哈希）。

> 例外：如果某个用户**当时正开着页面**，而你在两次刷新之间删掉了它引用的旧哈希文件，
> 那个页面的懒加载会 404。前端已经监听了 `vite:preloadError` 并自动整页刷新，所以表现为
> 「点了一下，页面自己刷新了」，不会白屏。

> ⚠️ **只更新前端这条只在信封契约没变时成立。** 后端的响应信封（`code` 的取值）改动后，
> 旧前端会拒绝新后端的每一个响应——表现为**所有请求都报「无法连接后端服务」**，
> 包括登录。前端会持续读出错（旧前端等 `code === 0`，新后端给状态码）。
>
> 此时必须**同时更新前端并让浏览器刷新**：`index.html` 是 `no-cache`，用户手动刷新一次即可；
> 如果 `index.html` 本身也被缓存了（比如中间有 CDN），要主动清缓存。
> 升级前后端二进制时顺带 `rsync` 一次 `web/` 最省事。

## 6. 排查

| 现象 | 先看什么 |
| --- | --- |
| 打开 `:8080` 看到 `{"code":404,"msg":"error.frontendDisabled"}` | 后端在纯 API 模式，没找到 `web/` 目录。看启动日志里那行「前端目录 ... 不存在」 |
| 前端提示「无法连接后端服务」 | 后端没启动，或代理配置错了。浏览器控制台会打出实际的 HTTP 状态与响应体 |
| 接口返回 500 | `journalctl -u mini-ruoyi`。500 一律带完整错误进日志，客户端只拿到一个键 |
| 所有人一起收到 429 | 前面有代理但没设 `APP_TRUSTED_PROXIES`，限流退化成全局了 |
| 进程莫名重启 | `dmesg \| grep -i oom`。多半是没设 `GOMEMLIMIT` |
| 服务监控页上 CPU / 内存显示「本平台读不到该指标」 | 正常。这两个指标读 `/proc`，只在 Linux 上有。磁盘和进程指标各平台都能读 |
| 登录后立刻被登出 | `APP_SECURE_COOKIE=true` 但在 HTTP 上访问，浏览器不会回传 cookie |

运行时状态可以直接看「系统监控」页面（CPU / 内存 / 磁盘 / Go 进程），
定时任务的状态在「系统工具 → 定时任务」，包括「上次执行成功了吗」和下次执行时间；
点某个任务的「执行历史」可以看它每一次跑的触发方式、结果与耗时。
