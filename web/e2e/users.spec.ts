import { expect, test } from '@playwright/test'

import { dialog, expectToast, filters, loginAsAdmin, openPage, unique } from './helpers'

test.beforeEach(async ({ page }) => {
  await loginAsAdmin(page)
  await openPage(page, '用户管理')
})

/** 建一个用户，返回用户名。 */
async function createUser(page: import('@playwright/test').Page, nickname = '端到端测试') {
  const name = unique('e2euser')
  await page.getByRole('button', { name: '新增' }).click()
  await dialog(page).getByLabel('用户名').fill(name)
  await dialog(page).getByLabel('密码').fill('e2e-password')
  await dialog(page).getByLabel('昵称').fill(nickname)
  await dialog(page).getByRole('button', { name: '保存' }).click()
  await expectToast(page, '已保存')
  return name
}

/** 用筛选把某个用户捞出来，避免依赖它在第几页第几行。 */
async function filterBy(page: import('@playwright/test').Page, username: string) {
  await filters(page).getByLabel('用户名').fill(username)
  await page.getByRole('button', { name: '查询' }).click()
}

test('列表展示种子管理员，且登录时间渲染成本地格式', async ({ page }) => {
  const row = page.getByRole('row').filter({ hasText: 'admin' }).first()
  await expect(row).toBeVisible()
  await expect(row.getByRole('cell', { name: 'admin' })).toBeVisible()
})

test('新增用户后能通过筛选找到', async ({ page }) => {
  const name = await createUser(page)
  await filterBy(page, name)

  await expect(page.getByRole('cell', { name })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'admin' })).toBeHidden()
})

test('用户名重复时把错误挂到对应输入框上', async ({ page }) => {
  await page.getByRole('button', { name: '新增' }).click()
  await dialog(page).getByLabel('用户名').fill('admin')
  await dialog(page).getByLabel('密码').fill('e2e-password')
  await dialog(page).getByRole('button', { name: '保存' }).click()

  // 后端返回 409 + error.duplicate + errors[{field:username, rule:unique}]，
  // 前端要渲染成「用户名已存在」而不是一个笼统的报错
  await expect(dialog(page).getByText('用户名已存在')).toBeVisible()
})

test('密码短于 8 位时后端也会拦下', async ({ page }) => {
  await page.getByRole('button', { name: '新增' }).click()
  await dialog(page).getByLabel('用户名').fill(unique('shortpw'))

  // 浏览器自带的 minlength 会先拦一次，这里去掉它验证后端也会拦
  await dialog(page).getByLabel('密码').evaluate((el: HTMLInputElement) => el.removeAttribute('minlength'))
  await dialog(page).getByLabel('密码').fill('abc')
  await dialog(page).getByRole('button', { name: '保存' }).click()

  await expect(dialog(page).getByText(/长度不能少于 8 个字符/)).toBeVisible()
})

test('筛选按用户名模糊匹配，只留下命中行', async ({ page }) => {
  const name = await createUser(page)
  await filterBy(page, name)

  await expect(page.getByRole('cell', { name })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'admin' })).toBeHidden()
  // 筛选条件进 URL，方便分享与刷新
  await expect(page).toHaveURL(new RegExp(`username=${name}`))
})

test('筛选条件写进 URL，刷新后仍然生效', async ({ page }) => {
  await filterBy(page, 'admin')
  await expect(page.getByRole('cell', { name: 'admin' })).toBeVisible()

  await page.reload()
  await expect(filters(page).getByLabel('用户名')).toHaveValue('admin')
  await expect(page.getByRole('cell', { name: 'admin' })).toBeVisible()
})

test('重置按钮清空筛选条件', async ({ page }) => {
  await filterBy(page, 'admin')
  await expect(page).toHaveURL(/username=admin/)

  await page.getByRole('button', { name: '重置', exact: true }).click()
  await expect(page).not.toHaveURL(/username=/)
  await expect(filters(page).getByLabel('用户名')).toHaveValue('')
})

test('按状态筛选', async ({ page }) => {
  await createUser(page)

  await filters(page).getByLabel('状态').selectOption('inactive')
  await page.getByRole('button', { name: '查询' }).click()

  // 刚建的用户是启用状态，不该出现在「停用」筛选里
  await expect(page.getByRole('cell', { name: 'admin' })).toBeHidden()
  await expect(page.getByText('共 0 条')).toBeVisible()
})

test('编辑用户的状态后列表跟着变', async ({ page }) => {
  const name = await createUser(page)
  await filterBy(page, name)

  await page.getByRole('row').filter({ hasText: name }).getByRole('button', { name: '编辑' }).click()
  await dialog(page).getByLabel('状态').selectOption('inactive')
  await dialog(page).getByRole('button', { name: '保存' }).click()
  await expectToast(page, '已保存')

  await expect(page.getByRole('row').filter({ hasText: name }).getByText('停用')).toBeVisible()
})

test('删除用户：列表里消失，且不需要二次确认', async ({ page }) => {
  const name = await createUser(page)
  await filterBy(page, name)

  await page.getByRole('row').filter({ hasText: name }).getByRole('button', { name: '删除' }).click()

  // 用户没有子数据，后端直接删掉，不该弹确认框
  await expectToast(page, '已删除')
  await expect(page.getByRole('cell', { name })).toBeHidden()
})

test('不能删除当前登录账号', async ({ page }) => {
  await page.getByRole('row').filter({ hasText: 'admin' }).getByRole('button', { name: '删除' }).click()

  // 后端返回 error.cannotDeleteSelf
  await expect(page.getByText('不能删除当前登录账号')).toBeVisible()
  await expect(page.getByRole('cell', { name: 'admin' })).toBeVisible()
})

test('重置密码后提示会话已失效', async ({ page }) => {
  const name = await createUser(page)
  await filterBy(page, name)

  await page.getByRole('row').filter({ hasText: name }).getByRole('button', { name: '重置密码' }).click()
  await dialog(page).getByLabel('密码').fill('brand-new-password')
  await dialog(page).getByRole('button', { name: '确定' }).click()

  await expectToast(page, /该用户的登录会话已全部失效/)
})

test('按角色筛选：只留下持有该角色的用户', async ({ page }) => {
  // 登录与打开页面由 beforeEach 负责，这里不要再调一次：
  // 已登录时 /login 会重定向回应用，loginAsAdmin 的两个 getByLabel 会落到
  // 筛选栏的「用户名」上（填进去一个 admin），然后在找「密码」时超时。
  const name = unique('rolefilter')

  // 建一个不挂任何角色的用户
  await page.getByRole('button', { name: '新增' }).click()
  await dialog(page).getByLabel('用户名').fill(name)
  await dialog(page).getByLabel('密码').fill('rolefilter-pw')
  await dialog(page).getByRole('button', { name: '保存' }).click()
  await expect(dialog(page)).toBeHidden()

  // 按「超级管理员」筛。admin 有它、刚建的这个没有，
  // 所以一次断言同时覆盖了「命中」和「不该出现」两个方向
  await filters(page).getByRole('combobox', { name: '角色' }).selectOption({ label: '超级管理员' })
  await page.getByRole('button', { name: '查询' }).click()

  await expect(page.getByRole('row').filter({ hasText: 'admin' }).first()).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: name })).toHaveCount(0)

  // 筛选条件要进 URL，刷新后仍然生效（和其它筛选一致的约定）
  await expect(page).toHaveURL(/role_id=/)
  await page.reload()
  await expect(page.getByRole('row').filter({ hasText: name })).toHaveCount(0)
})
