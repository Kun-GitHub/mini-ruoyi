import { expect, test } from '@playwright/test'

import { loginAsAdmin, sidebar, tabBar } from './helpers'

test.beforeEach(async ({ page }) => {
  await loginAsAdmin(page)
})

test('打开过的页面各占一个标签', async ({ page }) => {
  for (const title of ['用户管理', '角色管理', '菜单管理']) {
    await sidebar(page).getByRole('button', { name: title, exact: true }).click()
  }

  const tabs = tabBar(page).getByTestId('tab')
  await expect(tabs).toHaveCount(3)
  await expect(tabs.nth(0)).toHaveText(/用户管理/)
  await expect(tabs.nth(1)).toHaveText(/角色管理/)
  await expect(tabs.nth(2)).toHaveText(/菜单管理/)
})

test('重复点同一个菜单不会开出第二个标签', async ({ page }) => {
  await sidebar(page).getByRole('button', { name: '用户管理', exact: true }).click()
  await sidebar(page).getByRole('button', { name: '角色管理', exact: true }).click()
  await sidebar(page).getByRole('button', { name: '用户管理', exact: true }).click()

  await expect(tabBar(page).getByTestId('tab')).toHaveCount(2)
})

test('点标签切换页面，内容跟着换', async ({ page }) => {
  await sidebar(page).getByRole('button', { name: '用户管理', exact: true }).click()
  await expect(page.getByRole('cell', { name: 'admin' })).toBeVisible()

  await sidebar(page).getByRole('button', { name: '角色管理', exact: true }).click()
  await expect(page.getByRole('cell', { name: '超级管理员' })).toBeVisible()

  // 点回第一个标签
  await tabBar(page).getByTestId('tab').filter({ hasText: '用户管理' }).getByText('用户管理').click()
  await expect(page).toHaveURL(/\/system\/users$/)
  await expect(page.getByRole('cell', { name: 'admin' })).toBeVisible()
})

test('关闭当前标签后切到相邻标签', async ({ page }) => {
  await sidebar(page).getByRole('button', { name: '用户管理', exact: true }).click()
  await sidebar(page).getByRole('button', { name: '角色管理', exact: true }).click()

  await tabBar(page).getByTestId('tab').filter({ hasText: '角色管理' }).getByTestId('tab-close').click()

  await expect(tabBar(page).getByTestId('tab')).toHaveCount(1)
  // 关掉的是当前标签，应该自动切到剩下的那个
  await expect(page).toHaveURL(/\/system\/users$/)
})

test('关闭非当前标签时留在原页面', async ({ page }) => {
  await sidebar(page).getByRole('button', { name: '用户管理', exact: true }).click()
  await sidebar(page).getByRole('button', { name: '角色管理', exact: true }).click()

  await tabBar(page).getByTestId('tab').filter({ hasText: '用户管理' }).getByTestId('tab-close').click()

  await expect(tabBar(page).getByTestId('tab')).toHaveCount(1)
  await expect(page).toHaveURL(/\/system\/roles$/)
})

test('关闭其他：只留当前标签', async ({ page }) => {
  for (const title of ['用户管理', '角色管理', '菜单管理']) {
    await sidebar(page).getByRole('button', { name: title, exact: true }).click()
  }

  await page.getByRole('button', { name: '关闭其他' }).click()

  await expect(tabBar(page).getByTestId('tab')).toHaveCount(1)
  await expect(tabBar(page).getByTestId('tab')).toHaveText(/菜单管理/)
})

test('关闭全部后显示空态，且不会被自动弹回', async ({ page }) => {
  await sidebar(page).getByRole('button', { name: '用户管理', exact: true }).click()
  await sidebar(page).getByRole('button', { name: '角色管理', exact: true }).click()

  await page.getByRole('button', { name: '关闭全部' }).click()

  await expect(tabBar(page).getByTestId('tab')).toHaveCount(0)
  // 这一条是关键：如果外壳在「没有标签」时无条件跳转，
  // 用户点「关闭全部」会被立刻弹回第一个页面，等于关不掉
  await expect(page.getByText('从左侧菜单选择要打开的页面')).toBeVisible()
})

test('标签切换时页面状态保留', async ({ page }) => {
  // 在用户页填一个筛选条件
  await sidebar(page).getByRole('button', { name: '用户管理', exact: true }).click()
  await page.getByTestId('filters').getByLabel('用户名').fill('admin')
  await page.getByRole('button', { name: '查询' }).click()
  await expect(page.getByRole('cell', { name: 'admin' })).toBeVisible()

  // 切走再切回来
  await sidebar(page).getByRole('button', { name: '角色管理', exact: true }).click()
  await expect(page.getByRole('cell', { name: '超级管理员' })).toBeVisible()
  await tabBar(page).getByTestId('tab').filter({ hasText: '用户管理' }).getByText('用户管理').click()

  // 所有标签的页面是同时渲染、用 CSS 隐藏的，所以切回来时输入框内容还在
  await expect(page.getByTestId('filters').getByLabel('用户名')).toHaveValue('admin')
  await expect(page.getByRole('cell', { name: 'admin' })).toBeVisible()
})

test('登出会清空标签，避免下个账号看到上一个账号的页面', async ({ page }) => {
  await sidebar(page).getByRole('button', { name: '用户管理', exact: true }).click()
  await sidebar(page).getByRole('button', { name: '角色管理', exact: true }).click()
  await expect(tabBar(page).getByTestId('tab')).toHaveCount(2)

  await page.getByRole('button', { name: '退出登录' }).click()
  await loginAsAdmin(page)

  // 重新登录后只应有一个自动打开的标签
  await expect(tabBar(page).getByTestId('tab')).toHaveCount(1)
})
