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

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `APP_ADDR` | `:8080` | HTTP 监听地址 |
| `APP_DB_PATH` | `data.db` | SQLite 文件路径，相对**进程工作目录** |
| `APP_WEB_DIR` | 二进制同级 `web/`，不存在时 `./web` | 前端产物目录 |
| `APP_ENV` | `prod` | 设为 `dev` 使用 `gin.DebugMode`（输出路由表与警告） |

没有配置文件。

## API

所有接口都在 `/api/v1` 下。完整契约见 [../docs/architecture.md](../docs/architecture.md)。

### `GET /healthz`

探活，会真实 Ping 数据库。

```json
{"code":0,"msg":"ok","data":{"status":"up"}}
```

数据库不可达时返回 `503` + `{"code":1,"msg":"error.serviceUnavailable"}`。

### `POST /api/v1/devices`

```bash
curl -X POST localhost:8080/api/v1/devices \
  -H 'Content-Type: application/json' \
  -d '{"name":"sensor-1","location":"lab","enabled":true}'
```

| 字段 | 校验 |
| --- | --- |
| `name` | 必填，2–64 字符 |
| `location` | 必填 |
| `enabled` | 可选，默认 `false` |

成功 `200`，`data` 是新建的完整实体（`id` 与 `created_at` 由数据库生成后回填）：

```json
{"code":0,"msg":"ok","data":{"id":1,"name":"sensor-1","location":"lab","enabled":true,"created_at":"2025-09-26T16:32:05Z"}}
```

### `GET /api/v1/devices`

查询参数 `page`（默认 1，最小 1）、`page_size`（默认 20，范围 1–100，超出则回落 20）。
按 `id` 倒序。

```json
{"code":0,"msg":"ok","data":{"list":[],"total":0,"page":1,"page_size":20}}
```

空结果时 `list` 是 `[]`，不是 `null`。

### `GET /api/v1/devices/:id`

返回单个实体，不存在返回 `404` + `{"code":1,"msg":"error.notFound"}`。

### `PATCH /api/v1/devices/:id`

只支持改 `enabled`：

```bash
curl -X PATCH localhost:8080/api/v1/devices/1 \
  -H 'Content-Type: application/json' -d '{"enabled":false}'
```

`enabled` 是**必填**的。字段用指针接收，因此 `{}` 会返回 `400` 而不是把 `enabled` 静默改成 `false`：

```json
{"code":1,"msg":"error.validationFailed","errors":[{"field":"enabled","rule":"required"}]}
```

### `DELETE /api/v1/devices/:id`

```json
{"code":0,"msg":"ok","data":{"id":1}}
```

不返回 `204`——所有接口统一走响应信封，前端不必为它特判空 body。

### 错误响应

失败响应的 `msg` 是 **i18n 键**而非文案，文案由前端渲染。

```json
{"code":1,"msg":"error.validationFailed",
 "errors":[{"field":"name","rule":"min","param":"2"},
           {"field":"location","rule":"required"}]}
```

| HTTP | `msg` |
| --- | --- |
| 400 | `error.validationFailed` / `error.malformedBody` / `error.invalidId` |
| 404 | `error.notFound` |
| 413 | `error.bodyTooLarge` |
| 429 | `error.tooManyRequests` |
| 500 | `error.internal` |
| 503 | `error.serviceUnavailable` |

内置限制：请求体上限 1 MiB，按 IP 限流 20 rps（突发 40）。

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

- 认证（session / CSRF）与鉴权（RBAC）
- 用户 / 角色 / 菜单 / API 权限表
- 操作日志

`devices` 只是用于验证 CRUD、分页、错误契约与迁移链路的示例资源，不是业务功能。
