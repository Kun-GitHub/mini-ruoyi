import { expect, test } from '@playwright/test'

import { expectToast, loginAsAdmin, openPage, unique } from './helpers'

test.beforeEach(async ({ page }) => {
  await loginAsAdmin(page)
})

/**
 * 建一个「目录 + 子菜单」的组合，返回目录的标题键。
 *
 * 刻意不去删种子里的「系统管理」——那会把整个侧边栏导航弄没，
 * 后面所有用例都跟着失败，而且失败原因看起来跟菜单删除毫无关系。
 */
async function createDirectoryWithChild(page: import('@playwright/test').Page) {
  const suffix = unique('m')
  const dirKey = `menu.e2e.${suffix}`
  const childKey = `menu.e2e.${suffix}.child`

  await openPage(page, '菜单管理')

  await page.getByRole('button', { name: '新增' }).click()
  await page.getByRole('dialog').getByLabel('标题键').fill(dirKey)
  await page.getByRole('dialog').getByLabel('类型').selectOption('directory')
  await page.getByRole('dialog').getByRole('button', { name: '保存' }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expectToast(page, '已保存')

  // 子菜单必须挂在目录下，用界面上的上级下拉来选
  await page.getByRole('button', { name: '新增' }).click()
  await page.getByRole('dialog').getByLabel('标题键').fill(childKey)
  // 选刚建的那个目录：它的标题键就是唯一的展示文本
  await page.getByRole('dialog').getByLabel('上级菜单').selectOption({ label: dirKey })
  await page.getByRole('dialog').getByRole('button', { name: '保存' }).click()
  await expect(page.getByRole('dialog')).toBeHidden()

  return { dirKey, childKey }
}

test('删除有子菜单的目录会弹确认框并显示影响面', async ({ page }) => {
  await createDirectoryWithChild(page)

  // 找到刚建的目录那一行——它的标题键以 menu.e2e 开头
  const row = page.getByRole('row').filter({ hasText: /menu\.e2e\./ }).first()
  await row.getByRole('button', { name: '删除' }).click()

  // 后端返回 409 + error.hasDependents + {child_menus: N}。
  // 这不是错误，前端要弹确认框而不是报错。
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('确认删除')).toBeVisible()
  await expect(dialog.getByText('子菜单')).toBeVisible()
})

test('确认框点取消则什么都不删', async ({ page }) => {
  const { dirKey } = await createDirectoryWithChild(page)

  const before = await page.getByRole('row').count()

  const row = page.getByRole('row').filter({ hasText: dirKey }).first()
  await row.getByRole('button', { name: '删除' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '取消' }).click()

  await expect(page.getByRole('dialog')).toBeHidden()
  // 行数应该没变（目录和子菜单都还在）。
  // 这里用行数而不是 getByText(dirKey)：后者是子串匹配，
  // menu.e2e.x 会同时命中 menu.e2e.x.child 那一行，构成 strict violation。
  await expect(page.getByRole('row')).toHaveCount(before)
  await expect(page.getByRole('row').filter({ hasText: dirKey })).toHaveCount(2)
})

test('确认后目录连同子菜单一起消失', async ({ page }) => {
  const { dirKey, childKey } = await createDirectoryWithChild(page)

  const row = page.getByRole('row').filter({ hasText: dirKey }).first()
  await row.getByRole('button', { name: '删除' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '确定' }).click()

  await expectToast(page, '已删除')
  // 级联删除：子菜单跟着走，不留孤儿
  await expect(page.getByRole('row').filter({ hasText: childKey })).toHaveCount(0)
  await expect(page.getByRole('row').filter({ hasText: dirKey })).toHaveCount(0)
})

test('菜单挂在菜单下面会被拒绝', async ({ page }) => {
  await openPage(page, '菜单管理')

  // 种子里的「用户管理」是 menu 类型，不该能当上级
  await page.getByRole('button', { name: '新增' }).click()
  await page.getByRole('dialog').getByLabel('标题键').fill('menu.e2e.badparent')
  const options = await page.getByLabel('上级菜单').locator('option').allTextContents()
  // 下拉里只应出现目录，菜单不该作为候选。用「菜单名不出现」来断言，
  // 而不是「只包含系统管理」——其它用例可能留下自己建的目录。
  for (const menuTitle of ['用户管理', '角色管理', '菜单管理']) {
    expect(options.some((o) => o.includes(menuTitle))).toBe(false)
  }

  await page.getByRole('button', { name: '取消' }).click()
})

test('标题键必须以 menu. 开头', async ({ page }) => {
  await openPage(page, '菜单管理')

  await page.getByRole('button', { name: '新增' }).click()
  await page.getByRole('dialog').getByLabel('标题键').fill('not-a-menu-key')
  await page.getByRole('dialog').getByRole('button', { name: '保存' }).click()

  // 后端 binding:"startswith=menu." 会拦下它
  await expect(page.getByRole('dialog').getByText(/必须以 menu\. 开头/)).toBeVisible()
})
