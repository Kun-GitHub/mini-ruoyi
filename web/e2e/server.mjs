/**
 * E2E 用的后端进程。
 *
 * 每次跑测试都从空库开始：上一轮创建的用户、角色会残留下来，
 * 让「共 N 条」这类断言变得依赖执行顺序，非常难查。
 *
 * 依赖 bin/mini-ruoyi 与 bin/web 已由 `make build` 生成——
 * Makefile 的 test-e2e 目标会先构建。
 */
import { execSync, spawn } from 'node:child_process'
import { existsSync, rmSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(here, '..', '..')
const binDir = path.join(repoRoot, 'bin')
// Windows 上只能执行 .exe，Makefile 也会把产物建成这个名字
const binary = path.join(binDir, process.platform === 'win32' ? 'mini-ruoyi.exe' : 'mini-ruoyi')
const dbPath = path.join(binDir, 'e2e.db')

if (!existsSync(binary)) {
  console.error(`找不到 ${binary}，请先执行 make build`)
  process.exit(1)
}

// 端口被占时的报错要说清楚是「谁」占了：一个残留的旧进程会让 E2E
// 连着跑在旧代码上，或者直接启动失败并抛出一句难懂的 "address already in use"。
const PORT = 18111
try {
  const out = execSync(`lsof -nP -iTCP:${PORT} -sTCP:LISTEN -t`, { stdio: ['ignore', 'pipe', 'ignore'] })
    .toString()
    .trim()
  if (out) {
    console.error(
      `端口 ${PORT} 已被占用（PID ${out.split('\n').join(', ')}）。\n` +
        `大概率是上一次测试或手工启动留下的后端进程，先清掉：\n` +
        `  pkill -x mini-ruoyi`,
    )
    process.exit(1)
  }
} catch {
  // lsof 没输出 = 端口空闲，正常
}

for (const suffix of ['', '-wal', '-shm']) {
  rmSync(dbPath + suffix, { force: true })
}
rmSync(path.join(binDir, 'e2e-uploads'), { recursive: true, force: true })

const child = spawn(binary, [], {
  cwd: binDir,
  env: {
    ...process.env,
    APP_ADDR: '127.0.0.1:18111',
    APP_DB_PATH: dbPath,
    // 单独放一个上传目录，别和开发时的 uploads/ 混在一起
    APP_UPLOAD_DIR: path.join(binDir, 'e2e-uploads'),
    // 上限调到 1 MiB：测试要用一个 2 MiB 的构造文件验证「超限被拒」，
    // 用默认的 20 MiB 就得造 21 MiB，没必要
    APP_UPLOAD_MAX_MB: '1',
    APP_UPLOAD_QUOTA_MB: '8',
    // 放宽限流：整套用例是机器节奏在打接口，而默认的 20 rps 是给人手点的。
    // 不放宽的话，排在后面（比如分页用例狂建 22 个用户之后）的用例会偶发 429，
    // 表现为「表格是空的」这种和被测功能无关的失败。
    APP_RATE_LIMIT_RPS: '500',
    APP_RATE_LIMIT_BURST: '1000',
  },
  stdio: 'inherit',
})

// Playwright 停止 webServer 时发 SIGTERM，要转发给真正在监听的子进程，
// 否则后端会一直占着端口，下一次运行直接失败
for (const signal of ['SIGTERM', 'SIGINT']) {
  process.on(signal, () => child.kill(signal))
}
child.on('exit', (code) => process.exit(code ?? 0))
