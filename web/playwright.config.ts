import { defineConfig, devices } from '@playwright/test'

const PORT = 18111
const BASE_URL = `http://127.0.0.1:${PORT}`

export default defineConfig({
  testDir: './e2e',
  // 测试之间共享一个数据库，并行会互相干扰（比如「共 N 条」的断言）
  fullyParallel: false,
  workers: 1,
  // 失败重试会让「偶发」被掩盖掉。宁可红，也不要一个不稳定的绿灯。
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: BASE_URL,
    // 固定中文：断言基于文案，跟随运行环境的语言会让结果不可复现
    locale: 'zh-CN',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'node ./e2e/server.mjs',
    url: `${BASE_URL}/healthz`,
    reuseExistingServer: false,
    timeout: 60_000,
  },
})
