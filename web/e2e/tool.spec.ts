import { expect, test } from '@playwright/test'

import { expectToast, loginAsAdmin, openPage, sidebar, unique } from './helpers'

test.describe('定时任务', () => {
  test('列出代码里注册的全部任务，含下次执行时间', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '定时任务')

    // 任务来自代码注册表（internal/job），不是数据库里随便什么行
    for (const key of ['cleanup:expired_sessions', 'cleanup:old_logs', 'cleanup:orphan_files']) {
      await expect(page.getByText(key)).toBeVisible()
    }
    // 说明文案走 i18n，漏登记会显示原始键名
    await expect(page.getByText('清理过期会话')).toBeVisible()
    await expect(page.getByText(/^job\./)).toHaveCount(0)

    // 启用的任务应当有下次执行时间
    const row = page.getByRole('row').filter({ hasText: 'cleanup:expired_sessions' })
    await expect(row.getByText(/\d{4}\/\d{1,2}\/\d{1,2}/)).toBeVisible()
  })

  test('立即执行后能看到结果', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '定时任务')

    // 用单元格定位：「从未执行」在「上次执行」列，成功后在「上次结果」列变「成功」
    const row = page.getByRole('row').filter({ hasText: 'cleanup:orphan_files' })
    await expect(row.getByRole('cell', { name: '从未执行' })).toBeVisible()

    await row.getByRole('button', { name: '立即执行' }).click()
    await expectToast(page, /已触发/)

    // 执行是异步的，页面会在 1.5 秒后自动刷新一次
    await expect(row.getByRole('cell', { name: '成功' })).toBeVisible({ timeout: 10000 })
    await expect(row.getByRole('cell', { name: '从未执行' })).toHaveCount(0)
  })

  test('改 cron 立即生效，不需要重启', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '定时任务')

    const row = page.getByRole('row').filter({ hasText: 'cleanup:expired_sessions' })
    // 先钉住「只有一行」：任务清单以代码注册表为准，不该出现重复。
    // 不钉的话，一旦真有重复，后面的断言会命中多个元素而卡到超时，
    // 报出来的是「Test ended」这种看不出原因的错。
    await expect(row).toHaveCount(1)

    await row.getByRole('button', { name: '编辑' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('cron 表达式').fill('0 6 * * *')
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(dialog).toBeHidden()

    await expect(row.getByText('0 6 * * *')).toBeVisible()

    // 关键断言是**下次执行时间也要跟着变**。
    // 只断言 cron 文本变了不够——那只证明写进库了，没证明重新调度过，
    // 而「改完不用重启」正是这个功能的全部意义。
    // 列顺序：任务 0 / cron 1 / 状态 2 / 下次执行 3；0 6 * * * 的下次执行永远是 6:00。
    await expect(row.getByRole('cell').nth(3)).toHaveText(/6:00:00/)
  })

  test('非法 cron 被拒绝', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '定时任务')

    await page.getByRole('row').filter({ hasText: 'cleanup:old_logs' })
      .getByRole('button', { name: '编辑' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('cron 表达式').fill('不是cron')
    await dialog.getByRole('button', { name: '保存' }).click()

    await expectToast(page, 'cron 表达式不合法')
    // 弹窗不关，用户可以接着改
    await expect(dialog).toBeVisible()
  })

  test('停用后没有下次执行时间，启用后恢复', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '定时任务')

    const row = page.getByRole('row').filter({ hasText: 'cleanup:old_logs' })
    await row.getByRole('button', { name: '编辑' }).click()
    let dialog = page.getByRole('dialog')
    await dialog.getByLabel('状态').selectOption('inactive')
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(dialog).toBeHidden()

    await expect(row.getByText('停用')).toBeVisible()
    // 用列序号精确定位「下次执行」列：停用后它和「上次结果」列都是「—」，
    // getByText('—') 会命中两个（列顺序：任务 0 / cron 1 / 状态 2 / 下次执行 3）
    await expect(row.getByRole('cell').nth(3)).toHaveText('—')
  })

  test('能看到每次执行的记录', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '定时任务')

    // 挑一个其它用例不会触发的任务：这样「共 0 条」在整轮里都成立。
    // 用 cleanup:orphan_files 不行——上一个用例刚跑过它
    const row = page.getByRole('row').filter({ hasText: 'cleanup:expired_sessions' })

    // 全新库里还没有任何执行记录
    await row.getByRole('button', { name: '执行历史' }).click()
    let dialog = page.getByRole('dialog')
    await expect(dialog.getByText('共 0 条')).toBeVisible()
    await expect(dialog.getByText('暂无数据')).toBeVisible()
    await dialog.getByRole('button', { name: '关闭' }).click()
    await expect(dialog).toBeHidden()

    // 跑一次，历史里就该出现那一次
    await row.getByRole('button', { name: '立即执行' }).click()
    await expectToast(page, /已触发/)
    await expect(row.getByRole('cell', { name: '成功' })).toBeVisible({ timeout: 10000 })

    await row.getByRole('button', { name: '执行历史' }).click()
    dialog = page.getByRole('dialog')
    await expect(dialog.getByText('共 1 条')).toBeVisible()
    // 触发方式必须是「手动」：这是点了「立即执行」跑出来的，不是调度器跑出来的。
    // 两者分不开的话，「这个任务怎么今天跑了好几遍」就没法回答。
    await expect(dialog.getByRole('cell', { name: '手动' })).toBeVisible()
    await expect(dialog.getByRole('cell', { name: '成功' })).toBeVisible()
    await expect(dialog.getByRole('cell', { name: '定时' })).toHaveCount(0)
    // 新增的键漏登记的话，这里会原样显示 job.log.xxx
    await expect(dialog.getByText(/^job\./)).toHaveCount(0)
  })

  test('任务菜单在系统工具目录下', async ({ page }) => {
    await loginAsAdmin(page)
    await expect(sidebar(page).getByRole('button', { name: '系统工具', exact: true })).toBeVisible()
    for (const title of ['文件管理', '定时任务']) {
      await expect(sidebar(page).getByRole('button', { name: title, exact: true })).toBeVisible()
    }
  })
})

test.describe('文件管理', () => {
  test('上传后出现在列表，用量跟着涨', async ({ page }) => {
    const name = `${unique('e2e')}.txt`
    await loginAsAdmin(page)
    await openPage(page, '文件管理')

    await expect(page.getByText(/共 0 个文件/)).toBeVisible()

    await page.getByTestId('file-input').setInputFiles({
      name,
      mimeType: 'text/plain',
      buffer: Buffer.from('端到端测试用的内容'),
    })

    await expectToast(page, '上传完成')
    await expect(page.getByRole('cell', { name })).toBeVisible()
    await expect(page.getByText(/共 1 个文件/)).toBeVisible()
  })

  test('上传的文件可以下载，且文件名正确', async ({ page }) => {
    const name = `${unique('下载')}.txt`
    const body = '这是文件内容'
    await loginAsAdmin(page)
    await openPage(page, '文件管理')

    await page.getByTestId('file-input').setInputFiles({
      name,
      mimeType: 'text/plain',
      buffer: Buffer.from(body),
    })
    await expectToast(page, '上传完成')

    const row = page.getByRole('row').filter({ hasText: name })
    const [download] = await Promise.all([
      page.waitForEvent('download'),
      row.getByRole('link', { name: '下载' }).click(),
    ])

    // 中文文件名要靠 RFC 5987 编码才不乱码
    expect(download.suggestedFilename()).toBe(name)
    expect(await download.createReadStream().then(async (s) => {
      let out = ''
      for await (const chunk of s!) out += chunk.toString()
      return out
    })).toBe(body)
  })

  test('超过单文件上限被拒绝', async ({ page }) => {
    await loginAsAdmin(page)
    await openPage(page, '文件管理')

    // E2E 后端把上限设成 1 MiB
    await page.getByTestId('file-input').setInputFiles({
      name: 'too-big.bin',
      mimeType: 'application/octet-stream',
      buffer: Buffer.alloc(2 * 1024 * 1024),
    })

    await expectToast(page, '文件太大')
  })

  test('删除文件后列表与磁盘都清掉', async ({ page }) => {
    const name = `${unique('待删')}.txt`
    await loginAsAdmin(page)
    await openPage(page, '文件管理')

    await page.getByTestId('file-input').setInputFiles({
      name,
      mimeType: 'text/plain',
      buffer: Buffer.from('x'),
    })
    await expectToast(page, '上传完成')

    await page.getByRole('row').filter({ hasText: name })
      .getByRole('button', { name: '删除' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '确定' }).click()

    await expectToast(page, '已删除')
    await expect(page.getByRole('cell', { name })).toHaveCount(0)
  })

  test('按文件名筛选', async ({ page }) => {
    const name = unique('筛我')
    await loginAsAdmin(page)
    await openPage(page, '文件管理')

    await page.getByTestId('file-input').setInputFiles({
      name: `${name}.txt`,
      mimeType: 'text/plain',
      buffer: Buffer.from('x'),
    })
    await expectToast(page, '上传完成')

    await page.getByTestId('filters').getByLabel('文件名').fill(name)
    await page.getByRole('button', { name: '查询' }).click()

    await expect(page.getByRole('cell', { name: `${name}.txt` })).toBeVisible()
  })

  test('未登录不能下载', async ({ page, request }) => {
    // 直接用请求上下文：没有会话 cookie
    const res = await request.get('/api/v1/files/1/download')
    expect(res.status()).toBe(401)
  })
})
