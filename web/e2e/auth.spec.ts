import { expect, test } from '@playwright/test'

import { ADMIN, loginAsAdmin, sidebar, submitLogin } from './helpers'

test('未登录时深链接被送到登录页', async ({ page }) => {
  await page.goto('/system/users')
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
  // 地址栏也应该归到 /login，避免分享出去的链接看起来能直接打开
  await expect(page).toHaveURL(/\/login$/)
})

test('密码错误时显示后端返回的错误键对应的文案', async ({ page }) => {
  await submitLogin(page, ADMIN.username, 'definitely-wrong')
  // 后端返回 error.badCredentials，前端翻译成中文——断言的是翻译后的结果，
  // 所以这条用例同时验证了「键在字典里存在」
  await expect(page.getByText('用户名或密码错误')).toBeVisible()
  await expect(page.getByRole('button', { name: '退出登录' })).toBeHidden()
})

test('登录后侧边栏出现种子菜单', async ({ page }) => {
  await loginAsAdmin(page)

  await expect(sidebar(page).getByRole('button', { name: '系统管理', exact: true })).toBeVisible()
  for (const title of ['用户管理', '角色管理', '菜单管理']) {
    await expect(sidebar(page).getByRole('button', { name: title, exact: true })).toBeVisible()
  }
})

test('登录后自动落到第一个可访问页面而不是空白根路径', async ({ page }) => {
  await loginAsAdmin(page)
  await expect(page).toHaveURL(/\/system\/users$/)
})

test('刷新页面后会话仍然有效', async ({ page }) => {
  await loginAsAdmin(page)
  await page.reload()

  // cookie 还在，但 CSRF 令牌只在内存里，所以刷新后必须重新问一次 /auth/me
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
  await expect(sidebar(page).getByRole('button', { name: '系统管理', exact: true })).toBeVisible()
})

test('登出后回到登录页，且旧会话不可用', async ({ page }) => {
  await loginAsAdmin(page)
  await page.getByRole('button', { name: '退出登录' }).click()

  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()

  // 直接访问受保护路径应该还是被挡住
  await page.goto('/system/users')
  await expect(page).toHaveURL(/\/login$/)
})

test('切换语言后菜单文案跟着变，且刷新后保持', async ({ page }) => {
  await loginAsAdmin(page)

  await page.getByLabel('语言').selectOption('en-US')
  await expect(sidebar(page).getByRole('button', { name: 'System', exact: true })).toBeVisible()
  await expect(sidebar(page).getByRole('button', { name: 'Users', exact: true })).toBeVisible()

  // 语言存在 localStorage，刷新后应该还是英文
  await page.reload()
  await expect(sidebar(page).getByRole('button', { name: 'System', exact: true })).toBeVisible()
})
