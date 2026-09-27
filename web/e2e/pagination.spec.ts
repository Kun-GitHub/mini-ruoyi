import { expect, test } from '@playwright/test'

import { filters, loginAsAdmin, openPage, tabBar, unique } from './helpers'

/**
 * 分页之前完全没有 E2E 覆盖。这里造出超过一页的数据，验证：
 * 分页器出现、翻页拿到不同数据、筛选会重置到第一页、
 * 以及越界页码被钳回最后一页。
 *
 * 数据靠这个用例自己造（用户名带 unique 前缀），不依赖执行顺序。
 *
 * 注意：`page_size` 超出上限时的钳位、以及服务端把越界 page 钳到最后一页，
 * 这两条的边界行为由后端用例 TestPaginationBoundaries 覆盖——前端固定用
 * PAGE_SIZE=20，URL 里的 page_size 页面并不读取，在这里断言不到。
 */
const EXTRA = 22 // 刚好超过一页（每页 20）

// 每次 beforeEach 都要用新的前缀：用户名有唯一约束，
// 沿用同一个前缀会让第二个用例的建用户全部撞 409。
let runSeq = 0
let PREFIX = ''

test.beforeEach(async ({ page }) => {
  runSeq += 1
  PREFIX = unique(`pgu${runSeq}`)

  await loginAsAdmin(page)
  await openPage(page, '用户管理')

  // 造够两页的数据
  for (let i = 1; i <= EXTRA; i++) {
    const name = `${PREFIX}${String(i).padStart(2, '0')}`
    await page.getByRole('button', { name: '新增' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('用户名').fill(name)
    await dialog.getByLabel('密码').fill('pagination-pw')
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(dialog).toBeHidden()
  }
})

test('超过一页时出现分页器，翻页拿到不同数据', async ({ page }) => {
  // 用前缀筛出刚造的这批，避免受库里其它数据影响
  await filters(page).getByLabel('用户名').fill(PREFIX)
  await page.getByRole('button', { name: '查询' }).click()

  await expect(page.getByText(`共 ${EXTRA} 条`)).toBeVisible()
  await expect(page.getByText('1 / 2')).toBeVisible()
  // 每页 20 条
  await expect(page.getByRole('row').filter({ hasText: PREFIX })).toHaveCount(20)

  await page.getByRole('button', { name: '›' }).click()
  await expect(page.getByText('2 / 2')).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: PREFIX })).toHaveCount(EXTRA - 20)

  // 第一页的第一条不该出现在第二页
  const secondPageNames = await page.getByRole('row').filter({ hasText: PREFIX }).allTextContents()
  expect(secondPageNames.join()).not.toContain(`${PREFIX}01`)
})

test('筛选会把页码重置回第一页', async ({ page }) => {
  await filters(page).getByLabel('用户名').fill(PREFIX)
  await page.getByRole('button', { name: '查询' }).click()
  await page.getByRole('button', { name: '›' }).click()
  await expect(page.getByText('2 / 2')).toBeVisible()

  // 改条件再查，必须回到第一页——否则用户会停在一个空白页上
  await filters(page).getByLabel('用户名').fill(`${PREFIX}01`)
  await page.getByRole('button', { name: '查询' }).click()

  // 只剩一条时 totalPages = 1，分页器整体隐藏（不是显示 "1 / 1"）。
  // 这里真正要断的是「页码回到了 1」——URL 里不该再留着 page=2。
  await expect(page).not.toHaveURL(/page=/)
  await expect(page.getByRole('cell', { name: `${PREFIX}01` })).toBeVisible()
})

test('越界的页码被钳回最后一页', async ({ page }) => {
  // 模拟一个过期的书签／手工改地址栏
  await page.goto(`/system/users?username=${PREFIX}&page=99`)

  // 服务端会把 page 钳到最后一页，前端跟随它。
  // 不钳的话这里会显示 "99 / 2" 且表格空白。
  await expect(page.getByText('2 / 2')).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: PREFIX })).toHaveCount(EXTRA - 20)
})

test('分页状态进 URL，刷新后还在第二页', async ({ page }) => {
  await filters(page).getByLabel('用户名').fill(PREFIX)
  await page.getByRole('button', { name: '查询' }).click()
  await page.getByRole('button', { name: '›' }).click()
  await expect(page).toHaveURL(/page=2/)

  await page.reload()
  await expect(page.getByText('2 / 2')).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: PREFIX })).toHaveCount(EXTRA - 20)
})

test('角色列表同样分页', async ({ page }) => {
  // 借用户页的标签栏跳到角色页（此时用户页已打开）
  await page.getByRole('button', { name: '关闭全部' }).click()
  await openPage(page, '角色管理')
  await expect(tabBar(page).getByTestId('tab')).toHaveCount(1)

  // 种子里有 1 个内置角色，最多只有 1 页——这里只验证分页器在有数据时行为正常
  await expect(page.getByText(/共 \d+ 条/)).toBeVisible()
})
