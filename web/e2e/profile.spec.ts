import { expect, test } from '@playwright/test'

import { activePanel, expectToast, loginAsAdmin, openPage, sidebar, submitLogin, unique } from './helpers'

/** 从顶栏进个人中心。它不是菜单项，入口在右上角的用户名。 */
async function openProfile(page: import('@playwright/test').Page): Promise<void> {
  await page.getByTestId('profile-link').click()
  await expect(page.getByTestId('tab-bar').getByTestId('tab').filter({ hasText: '个人中心' })).toBeVisible()
}

test('个人中心不在菜单里，入口在顶栏用户名', async ({ page }) => {
  await loginAsAdmin(page)

  // 侧边栏里不该有它：菜单是权限管理的东西，而改自己的密码是每个账号的基本能力
  await expect(sidebar(page).getByRole('button', { name: '个人中心', exact: true })).toHaveCount(0)

  await openProfile(page)
  // shadcn 的 CardTitle 渲染成 div 而不是标题元素，所以按文本找
  await expect(activePanel(page).getByText('基本资料')).toBeVisible()
  await expect(activePanel(page).getByText('修改密码')).toBeVisible()
})

test('用户名只读，不能自己改', async ({ page }) => {
  await loginAsAdmin(page)
  await openProfile(page)

  await expect(activePanel(page).getByLabel('用户名')).toBeDisabled()
  await expect(activePanel(page).getByLabel('用户名')).toHaveValue('admin')
})

test('改资料后顶栏显示新昵称', async ({ page }) => {
  const nick = unique('昵称')
  await loginAsAdmin(page)
  await openProfile(page)

  await activePanel(page).getByLabel('昵称').fill(nick)
  await activePanel(page).getByRole('button', { name: '保存' }).first().click()

  await expectToast(page, '已保存')
  // 顶栏读的是会话里的昵称，保存后必须重新拉一次会话才不是旧值
  await expect(page.getByTestId('profile-link')).toHaveText(nick)
})

test('两次新密码不一致时前端就拦住', async ({ page }) => {
  await loginAsAdmin(page)
  await openProfile(page)

  await activePanel(page).getByLabel('当前密码').fill('admin123')
  await activePanel(page).getByLabel('新密码', { exact: true }).fill('brand-new-password')
  await activePanel(page).getByLabel('确认新密码').fill('different-password')
  await activePanel(page).getByRole('button', { name: '保存' }).nth(1).click()

  await expect(activePanel(page).getByText('两次输入的新密码不一致')).toBeVisible()
})

test('旧密码不对时后端拒绝', async ({ page }) => {
  await loginAsAdmin(page)
  await openProfile(page)

  await activePanel(page).getByLabel('当前密码').fill('definitely-wrong')
  await activePanel(page).getByLabel('新密码', { exact: true }).fill('brand-new-password')
  await activePanel(page).getByLabel('确认新密码').fill('brand-new-password')
  await activePanel(page).getByRole('button', { name: '保存' }).nth(1).click()

  await expectToast(page, '当前密码不正确')
})

test('改密码后当前会话保留，其他会话被踢', async ({ page, browser }) => {
  const name = unique('pwduser')
  await loginAsAdmin(page)

  // 建一个无任何权限的用户，验证「不需要管理权限」这一点
  await openPage(page, '用户管理')
  await page.getByRole('button', { name: '新增' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('用户名').fill(name)
  await dialog.getByLabel('密码').fill('original-password')
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(dialog).toBeHidden()

  // 另开一个上下文登录同一账号，制造第二条会话
  const other = await browser.newContext({ locale: 'zh-CN' })
  const otherPage = await other.newPage()
  await submitLogin(otherPage, name, 'original-password')
  await expect(otherPage.getByRole('button', { name: '退出登录' })).toBeVisible()

  // 在第一个上下文里改密码
  await page.getByRole('button', { name: '退出登录' }).click()
  await submitLogin(page, name, 'original-password')
  await openProfile(page)
  await activePanel(page).getByLabel('当前密码').fill('original-password')
  await activePanel(page).getByLabel('新密码', { exact: true }).fill('changed-password')
  await activePanel(page).getByLabel('确认新密码').fill('changed-password')
  await activePanel(page).getByRole('button', { name: '保存' }).nth(1).click()
  await expectToast(page, /其他设备上的登录已被登出/)

  // 当前会话必须还在：改完密码立刻被登出，用户会以为改失败了
  await page.reload()
  await expect(page.getByTestId('profile-link')).toBeVisible()

  // 另一个上下文应当被踢出去
  await otherPage.reload()
  await expect(otherPage.getByRole('button', { name: '登录', exact: true })).toBeVisible()

  await other.close()
})
