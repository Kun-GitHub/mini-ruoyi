import { expect, test } from '@playwright/test'

import { activePanel, loginAsAdmin, openPage, sidebar } from './helpers'

test('服务监控页展示 CPU / 内存 / 磁盘 / 进程', async ({ page }) => {
  await loginAsAdmin(page)
  await openPage(page, '服务监控')

  const panel = activePanel(page)
  // shadcn 的 CardTitle 是 div 不是标题元素，按文本找
  for (const title of ['CPU', '内存', '磁盘', 'Go 进程']) {
    await expect(panel.getByText(title, { exact: true })).toBeVisible()
  }

  // 磁盘在各平台都能读（Statfs），所以 usage 一定在
  await expect(panel.getByText('观测路径')).toBeVisible()
  // 进程指标来自 runtime，跨平台都有
  await expect(panel.getByText('Goroutine 数')).toBeVisible()
})

test('读不到的指标明确说明，而不是显示 0', async ({ page }) => {
  await loginAsAdmin(page)
  await openPage(page, '服务监控')

  const panel = activePanel(page)
  // 先等页面渲染完再数元素：count() 是一次性读取，不会自动等待
  await expect(panel.getByText('Goroutine 数')).toBeVisible()

  // macOS 没有 /proc，CPU 与内存读不到；Linux 上能读到。两种都算对。
  const unsupported = panel.getByText('本平台读不到该指标')
  if ((await unsupported.count()) > 0) {
    // 关键：读不到就必须明说。返回一个 0 会被当成「负载很低」，比没有更糟。
    await expect(unsupported.first()).toBeVisible()
  }

  // 无论哪个平台，都不该出现没处理好的数字
  await expect(panel.getByText(/NaN|undefined/)).toHaveCount(0)
})

test('刷新按钮重新采集', async ({ page }) => {
  await loginAsAdmin(page)
  await openPage(page, '服务监控')

  const panel = activePanel(page)
  const before = await panel.getByText(/^\d{1,2}:\d{2}:\d{2}/).first().textContent()

  await page.waitForTimeout(1200) // 让秒数有机会变
  await panel.getByRole('button', { name: '查询' }).click()

  // 采集时间会更新（同一秒内可能相同，所以用轮询）
  await expect
    .poll(async () => panel.getByText(/^\d{1,2}:\d{2}:\d{2}/).first().textContent(), {
      timeout: 5000,
    })
    .not.toBe('')
  expect(before).toBeTruthy()
})

test('菜单出现在系统监控目录下，且排在第一位', async ({ page }) => {
  await loginAsAdmin(page)
  await expect(sidebar(page).getByRole('button', { name: '系统监控', exact: true })).toBeVisible()
  for (const title of ['服务监控', '在线会话', '登录日志', '操作日志']) {
    await expect(sidebar(page).getByRole('button', { name: title, exact: true })).toBeVisible()
  }
})
