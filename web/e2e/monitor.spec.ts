import { expect, test, type Locator, type Page } from '@playwright/test'

import { dialog, expectToast, loginAsAdmin, logout, openPage, sidebar, unique } from './helpers'

/**
 * 系统监控三页：在线会话 / 登录日志 / 操作日志。
 *
 * 日志是**批量落库**的（2 秒刷一次盘），所以断言必须能等。
 * Playwright 的 toBeVisible 自带 5 秒重试，正好覆盖这个窗口——
 * 不要用 waitForTimeout 硬等，那样既慢又不稳。
 */

/**
 * 等一条日志出现在表格里。
 *
 * 必须「重新加载 + 重试」，不能只靠 toBeVisible 的自动重试：
 * 日志是批量落库的（最多 2 秒延迟），而页面在挂载时只拉一次数据。
 * 定位器的自动重试只是重新求值 DOM，不会重新发请求——数据没到就永远等不到。
 */
async function expectLogRow(page: Page, row: Locator, timeout = 20000): Promise<void> {
  await expect(async () => {
    await page.reload()
    await expect(row.first()).toBeVisible({ timeout: 1000 })
  }).toPass({ timeout })
}

test.describe('在线会话', () => {
  test('列出当前登录的会话，并标出自己的那条', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '在线会话')

    const row = page.getByRole('row').filter({ hasText: 'admin' }).first()
    await expect(row).toBeVisible()
    // 自己的会话要有标记：踢自己会报错，先让人看出来
    await expect(row.getByText('当前会话')).toBeVisible()
    // 自己那条的「强制下线」按钮应当不可点
    await expect(row.getByRole('button', { name: '强制下线' })).toBeDisabled()
  })

  test('踢掉另一个会话后对方立刻失效', async ({ page, browser }) => {
    const name = unique('kickme')
    await loginAsAdmin(page)
    await openPage(page, '用户管理')

    // 建一个用户
    await page.getByRole('button', { name: '新增' }).click()
    await dialog(page).getByLabel('用户名').fill(name)
    await dialog(page).getByLabel('密码').fill('kick-password')
    await dialog(page).getByRole('button', { name: '保存' }).click()
    await expect(dialog(page)).toBeHidden()

    // 用另一个浏览器上下文登录，产生第二条会话
    const other = await browser.newContext({ locale: 'zh-CN' })
    const otherPage = await other.newPage()
    await otherPage.goto('/login')
    await otherPage.getByLabel('用户名').fill(name)
    await otherPage.getByLabel('密码').fill('kick-password')
    await otherPage.getByRole('button', { name: '登录', exact: true }).click()
    await expect(otherPage.getByRole('button', { name: '退出登录' })).toBeVisible()

    // 在会话列表里踢掉它
    await openPage(page, '在线会话')
    const row = page.getByRole('row').filter({ hasText: name }).first()
    await expect(row).toBeVisible()
    await row.getByRole('button', { name: '强制下线' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '确定' }).click()
    await expectToast(page, '已强制下线')

    // 对方下一次请求就被登出——这就是「服务端持有会话状态」的意义
    await otherPage.reload()
    await expect(otherPage.getByRole('button', { name: '登录', exact: true })).toBeVisible()

    await other.close()
  })

  test('用户列表上可以一次强退该用户的全部会话', async ({ page, browser }) => {
    const name = unique('kickall')
    await loginAsAdmin(page)
    await openPage(page, '用户管理')

    await page.getByRole('button', { name: '新增' }).click()
    await dialog(page).getByLabel('用户名').fill(name)
    await dialog(page).getByLabel('密码').fill('kick-password')
    await dialog(page).getByRole('button', { name: '保存' }).click()
    await expect(dialog(page)).toBeHidden()

    // 同一账号在两个上下文里各登录一次 → 两条会话。
    // 这正是「只能在会话列表里一条条踢」不够用的场景：
    // 账号被盗时要的是把这个人的所有会话一起踢掉。
    const one = await browser.newContext({ locale: 'zh-CN' })
    const two = await browser.newContext({ locale: 'zh-CN' })
    for (const ctx of [one, two]) {
      const p = await ctx.newPage()
      await p.goto('/login')
      await p.getByLabel('用户名').fill(name)
      await p.getByLabel('密码').fill('kick-password')
      await p.getByRole('button', { name: '登录', exact: true }).click()
      await expect(p.getByRole('button', { name: '退出登录' })).toBeVisible()
    }

    // 自己那一行的按钮是置灰的：后端会报 cannotKickSelf
    const selfRow = page.getByRole('row').filter({ hasText: 'admin' }).first()
    await expect(selfRow.getByRole('button', { name: '强制下线' })).toBeDisabled()

    const row = page.getByRole('row').filter({ hasText: name }).first()
    await row.getByRole('button', { name: '强制下线' }).click()

    // 确认框的标题是「确认强制下线」而不是通用的「确认删除」——
    // 这里什么都没删，标题写错会让人以为删了东西
    await expect(dialog(page).getByText('确认强制下线')).toBeVisible()
    await dialog(page).getByRole('button', { name: '确定' }).click()
    await expectToast(page, '已强制下线')

    // 两条会话都应当失效，而不是只死一条
    for (const ctx of [one, two]) {
      const p = ctx.pages()[0]
      await p.reload()
      await expect(p.getByRole('button', { name: '登录', exact: true })).toBeVisible()
      await ctx.close()
    }

    // 发起者自己不受影响
    await page.reload()
    await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
  })
})

test.describe('登录日志', () => {
  test('成功与失败都记录，失败带上原因', async ({ page }) => {
    // 先造一次失败登录（在登录页上做，还没进后台）
    await page.goto('/login')
    await page.getByLabel('用户名').fill('admin')
    await page.getByLabel('密码').fill('definitely-wrong')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.getByText('用户名或密码错误')).toBeVisible()

    // 再正常登录
    await loginAsAdmin(page)
    await openPage(page, '登录日志')

    // 失败那条：原因用的是后端的 i18n 键，前端翻译成文案
    // 用「状态单元格」定位，而不是 hasText: '失败'——
    // 后者会命中表头行（里面有「失败原因」这一列名）
    const failed = page
      .getByRole('row')
      .filter({ has: page.getByRole('cell', { name: '失败', exact: true }) })
    await expectLogRow(page, failed)
    await expect(failed.first().getByText('用户名或密码错误')).toBeVisible()

    // 成功那条
    await expectLogRow(
      page,
      page.getByRole('row').filter({ has: page.getByRole('cell', { name: '成功', exact: true }) }),
    )
  })

  test('按状态筛选', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '登录日志')

    // 用 role 定位而不是 getByLabel：页面上「状态」既是表头列名也是筛选标签，
    // getByLabel 在这个页面上会解析出多个元素
    await page.getByTestId('filters').getByRole('combobox', { name: '状态' }).selectOption('failed')
    await page.getByRole('button', { name: '查询' }).click()

    // 筛「失败」时不该出现成功记录
    await expect(page.getByRole('row').filter({ hasText: '成功' })).toHaveCount(0)
  })

  test('时间范围筛选：选今天能查到，选明天查不到', async ({ page }) => {
    // 先在登录页造一条记录，再进后台
    await page.goto('/login')
    await page.getByLabel('用户名').fill('admin')
    await page.getByLabel('密码').fill('definitely-wrong')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.getByText('用户名或密码错误')).toBeVisible()
    await loginAsAdmin(page)
    await openPage(page, '登录日志')

    const iso = (d: Date) =>
      `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    const today = iso(new Date())
    const tomorrow = iso(new Date(Date.now() + 24 * 3600 * 1000))

    // 时间范围是筛选栏里最后两个输入框
    const dates = page.getByTestId('filters').getByRole('textbox')
    const query = () => page.getByRole('button', { name: '查询' }).click()

    // ① 「今天到明天」必须命中。
    // 用 expectLogRow 而不是裸的 toBeVisible：日志批量落库、最多 2 秒延迟，
    // 而 toBeVisible 的自动重试只重新求值 DOM，不会重新发请求——
    // 实测两条登录与查询全落在同一秒内，直接断言会稳定失败。
    await dates.nth(1).fill(today)
    await dates.nth(2).fill(tomorrow)
    await query()
    await expectLogRow(page, page.getByRole('row').filter({ hasText: 'admin' }))

    // ② 确认有数据之后再断言为空，否则「为空」可能是假的——
    // 日志还没落库时任何查询都是空的，那个绿灯什么也没证明。
    //
    // 这里才是 end 含当天的真正考验：start 落在将来时，
    // end 若没 +1 天变成开区间上界，边界会往外溜一天。
    await dates.nth(1).fill(tomorrow)
    await dates.nth(2).fill(tomorrow)
    await query()
    await expect(page.getByText(/共 0 条/)).toBeVisible()
  })
})

test.describe('操作日志', () => {
  test('记录写操作，不记录读操作', async ({ page }) => {
    const name = unique('oplogme')
    await loginAsAdmin(page)
    await openPage(page, '用户管理')

    // 一次写操作
    await page.getByRole('button', { name: '新增' }).click()
    await dialog(page).getByLabel('用户名').fill(name)
    await dialog(page).getByLabel('密码').fill('oplog-password')
    await dialog(page).getByRole('button', { name: '保存' }).click()
    await expect(dialog(page)).toBeHidden()

    // 一次读操作（刷新列表）——它不该出现在日志里
    await page.reload()

    await openPage(page, '操作日志')

    // 用路径单元格精确匹配，不能用 filter({ hasText: '/api/v1/users' })——
    // hasText 是**子串**匹配，它会同时命中 '/api/v1/users/:id/sessions' 这类路径，
    // 而日志按时间倒序，.first() 拿到的可能是那条，它里面没有 POST。
    const row = page
      .getByRole('row')
      .filter({ has: page.getByRole('cell', { name: '/api/v1/users', exact: true }) })
    await expectLogRow(page, row)
    await expect(row.first().getByText('POST')).toBeVisible()

    // 关键：GET 不记。记了的话日志量会随浏览行为膨胀，而 GET 不是「操作」。
    await expect(page.getByRole('row').filter({ hasText: 'GET' })).toHaveCount(0)
  })

  test('被拒绝的请求也留痕，并带上原因', async ({ page }) => {
    const name = unique('denied')
    await loginAsAdmin(page)

    // 建一个没有任何权限的用户
    await openPage(page, '用户管理')
    await page.getByRole('button', { name: '新增' }).click()
    await dialog(page).getByLabel('用户名').fill(name)
    await dialog(page).getByLabel('密码').fill('denied-password')
    await dialog(page).getByRole('button', { name: '保存' }).click()
    await expect(dialog(page)).toBeHidden()

    // 用他登录并尝试越权
    await logout(page)
    await page.getByLabel('用户名').fill(name)
    await page.getByLabel('密码').fill('denied-password')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()

    // 侧边栏没有菜单，所以直接调接口试——界面藏了按钮不等于接口被拦。
    // 必须带上 CSRF 头：不带的话会先被 CSRF 中间件以 403 csrfInvalid 拦下，
    // 测到的是另一回事，而不是「越权」。
    const denied = await page.evaluate(async () => {
      const me = await (await fetch('/api/v1/auth/me')).json()
      const res = await fetch('/api/v1/users', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': me.data.csrf_token,
        },
        body: JSON.stringify({ username: 'sneaky-user', password: 'sneaky-password' }),
      })
      return { status: res.status, msg: (await res.json()).msg }
    })
    // 先确认「越权」确实被拦下了，而且**拦在权限这一层**。
    // 只断 403 不够：CSRF 失败也是 403，而它在操作日志之前，
    // 那样测到的是另一回事，日志里当然没有。
    expect(denied, `越权请求的实际响应: ${JSON.stringify(denied)}`).toEqual({
      status: 403,
      msg: 'error.forbidden',
    })

    // 换回管理员看日志
    await page.getByRole('button', { name: '退出登录' }).click()
    await loginAsAdmin(page)
    await openPage(page, '操作日志')

    // 同时按用户名和路径筛：那个用户还有一条「登出」记录（状态 200、无原因），
    // 只按用户名筛的话 .first() 会取到它
    const row = page.getByRole('row').filter({ hasText: name }).filter({ hasText: '/api/v1/users' })
    await expectLogRow(page, row)
    // 越权试探是最该留痕的，原因也要看得懂
    await expect(row.first().getByText('没有操作权限')).toBeVisible()
  })

  test('按方法筛选', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '操作日志')

    await page.getByTestId('filters').getByRole('combobox', { name: '方法' }).selectOption('DELETE')
    await page.getByRole('button', { name: '查询' }).click()

    await expect(page.getByRole('row').filter({ hasText: 'POST' })).toHaveCount(0)
  })

  test('菜单出现在系统监控目录下', async ({ page }) => {
    await loginAsAdmin(page)
    await expect(sidebar(page).getByRole('button', { name: '系统监控', exact: true })).toBeVisible()
    for (const title of ['在线会话', '登录日志', '操作日志']) {
      await expect(sidebar(page).getByRole('button', { name: title, exact: true })).toBeVisible()
    }
  })
})
