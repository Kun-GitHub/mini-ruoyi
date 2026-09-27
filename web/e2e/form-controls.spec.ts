import { expect, test } from '@playwright/test'

import { filters, loginAsAdmin, openPage } from './helpers'

/**
 * 表单控件的尺寸约规。
 *
 * 这些断言量的是**浏览器里的实际像素**，不是 class 字符串——
 * 高度不一致这类问题的表现是「文本框比下拉框矮一截」，
 * 只有真的量一次才能保证它不再回来（换了主题预设、升级了 shadcn 组件都可能打破它）。
 */

/**
 * 筛选栏里的文本类控件。
 *
 * 排除 checkbox：它本来就是小方框，不该跟输入框比高度。
 */
function controls(page: import('@playwright/test').Page) {
  return filters(page).locator('input:not([type=checkbox]), select, button')
}

test('筛选栏里的文本框、下拉框、按钮高度一致', async ({ page }) => {
  await loginAsAdmin(page)

  for (const title of ['用户管理', '角色管理', '登录日志', '操作日志']) {
    await openPage(page, title)
    await expect(filters(page)).toBeVisible()

    const heights = await controls(page).evaluateAll((els) =>
      els.map((el) => ({
        tag: el.tagName.toLowerCase(),
        h: Math.round(el.getBoundingClientRect().height),
      })),
    )

    expect(heights.length).toBeGreaterThan(1)
    const distinct = [...new Set(heights.map((x) => x.h))]
    expect(
      distinct,
      `「${title}」筛选栏里控件高度不一致：${JSON.stringify(heights)}`,
    ).toHaveLength(1)
    // 顺带钉住绝对值：h-7 = 28px（浏览器取整可能是 27）
    expect(distinct[0]).toBeGreaterThanOrEqual(27)
    expect(distinct[0]).toBeLessThanOrEqual(28)
  }
})

test('输入框与同页弹窗里的下拉框高度也一致', async ({ page }) => {
  await loginAsAdmin(page)
  await openPage(page, '用户管理')

  await page.getByRole('button', { name: '新增' }).click()
  const dialog = page.getByRole('dialog')

  const heights = await dialog
    .locator('input:not([type=checkbox]), select')
    .evaluateAll((els) => els.map((el) => Math.round(el.getBoundingClientRect().height)))
  const distinct = [...new Set(heights)]
  expect(distinct, `弹窗里控件高度不一致：${JSON.stringify(heights)}`).toHaveLength(1)
  expect(distinct[0]).toBeGreaterThanOrEqual(27)
  expect(distinct[0]).toBeLessThanOrEqual(28)
})

test('筛选字段是固定宽度，不随屏幕变宽而被拉长', async ({ page }) => {
  await loginAsAdmin(page)
  await openPage(page, '用户管理')

  const input = filters(page).getByLabel('用户名')
  const before = await input.boundingBox()
  expect(before).not.toBeNull()

  // 等分栅格会把字段拉满可用宽度；固定宽度下换个宽屏不该变
  await page.setViewportSize({ width: 1600, height: 900 })
  const after = await input.boundingBox()
  expect(after).not.toBeNull()

  expect(Math.abs(after!.width - before!.width)).toBeLessThanOrEqual(1)
  expect(before!.width).toBeLessThan(260)
})

test('窄屏下字段换行而不是被压扁', async ({ page }) => {
  await loginAsAdmin(page)
  await openPage(page, '用户管理')

  const wide = await filters(page).boundingBox()
  await page.setViewportSize({ width: 600, height: 900 })
  const narrow = await filters(page).boundingBox()

  expect(wide).not.toBeNull()
  expect(narrow).not.toBeNull()
  // 换行的表现是容器变高（多出一行），而不是每个字段变窄
  expect(narrow!.height).toBeGreaterThan(wide!.height)

  // 字段宽度仍然保持不变
  const input = await filters(page).getByLabel('用户名').boundingBox()
  expect(Math.abs(input!.width - 192)).toBeLessThanOrEqual(2) // w-48 = 12rem = 192px
})
