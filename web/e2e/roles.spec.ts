import { expect, test } from '@playwright/test'

import { dialog, expectToast, loginAsAdmin, logout, openPage, sidebar, submitLogin, unique } from './helpers'

test.beforeEach(async ({ page }) => {
  await loginAsAdmin(page)
})

test('授权界面列出全部权限码，且文案来自前端字典', async ({ page }) => {
  const code = unique('grantrole')

  await openPage(page, '角色管理')
  await page.getByRole('button', { name: '新增' }).click()
  await dialog(page).getByLabel('角色编码').fill(code)
  await dialog(page).getByLabel('名称').fill('授权测试角色')
  await dialog(page).getByRole('button', { name: '保存' }).click()
  await expectToast(page, '已保存')

  await page.getByRole('row').filter({ hasText: code }).getByRole('button', { name: '授权' }).click()

  await expect(dialog(page)).toBeVisible()

  // 后端只返回权限码与 label_key，文案由前端字典渲染。
  // 如果某个键漏登记，界面会显示原始键名（perm.system.user.list）而不是中文。
  await expect(dialog(page).getByText('查询用户')).toBeVisible()
  await expect(dialog(page).getByText('重置密码')).toBeVisible()
  await expect(dialog(page).getByText(/^perm\./)).toBeHidden()
})

test('授予权限后该用户能访问对应接口，未授予的仍然是 403', async ({ page }) => {
  const roleCode = unique('limited')
  const userName = unique('limiteduser')

  // 建角色，只给「查询用户」
  await openPage(page, '角色管理')
  await page.getByRole('button', { name: '新增' }).click()
  await dialog(page).getByLabel('角色编码').fill(roleCode)
  await dialog(page).getByLabel('名称').fill('只读角色')
  await dialog(page).getByRole('button', { name: '保存' }).click()
  await expectToast(page, '已保存')

  // 菜单授权和接口权限是两件事：菜单决定侧边栏有没有入口，
  // 权限码决定接口能不能调。这里两者都给上，才是能正常使用的组合。
  await page.getByRole('row').filter({ hasText: roleCode }).getByRole('button', { name: '授权' }).click()
  await dialog(page).getByRole('checkbox', { name: '用户管理' }).check()
  await dialog(page).getByRole('checkbox', { name: '查询用户' }).check()
  await dialog(page).getByRole('button', { name: '保存' }).click()
  // 用「弹窗关闭」而不是「出现已保存通知」来判断保存成功：
  // 通知是叠加的，上一次操作留下的那条会让断言立刻通过，
  // 即使这次的请求其实失败了
  await expect(dialog(page)).toBeHidden()

  // 建用户并绑角色
  await openPage(page, '用户管理')
  await page.getByRole('button', { name: '新增' }).click()
  await dialog(page).getByLabel('用户名').fill(userName)
  await dialog(page).getByLabel('密码').fill('limited-password')
  await dialog(page).getByRole('checkbox', { name: '只读角色' }).check()
  await dialog(page).getByRole('button', { name: '保存' }).click()
  await expectToast(page, '已保存')

  // 换成这个用户登录
  await logout(page)
  await submitLogin(page, userName, 'limited-password')

  // 授过「用户管理」菜单，所以能进这个页面
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
  await expect(sidebar(page).getByRole('button', { name: '用户管理', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'admin' })).toBeVisible()

  // 没有 user:add，新增按钮不该出现。
  // 注意「隐藏按钮」只是界面层面的收敛，真正的拦截在后端（同一个账号直接调
  // POST /users 会拿到 403，这条由后端用例覆盖）。
  await expect(page.getByRole('button', { name: '新增' })).toBeHidden()
  // 没有授权菜单也没有 role:list，所以侧边栏里不该有角色管理
  await expect(sidebar(page).getByRole('button', { name: '角色管理', exact: true })).toBeHidden()
})

test('勾选状态与已保存的授权一致', async ({ page }) => {
  const roleCode = unique('roundtrip')

  await openPage(page, '角色管理')
  await page.getByRole('button', { name: '新增' }).click()
  await dialog(page).getByLabel('角色编码').fill(roleCode)
  await dialog(page).getByLabel('名称').fill('回填测试')
  await dialog(page).getByRole('button', { name: '保存' }).click()
  await expectToast(page, '已保存')

  await page.getByRole('row').filter({ hasText: roleCode }).getByRole('button', { name: '授权' }).click()

  // 用可访问名字定位勾选框：<label> 包着 input 和文案，勾选框的名字就是文案。
  // 「先找文本再取父节点」在嵌套结构下会一次命中多个元素。
  const userList = dialog(page).getByRole('checkbox', { name: '查询用户' })
  await userList.check()
  await dialog(page).getByRole('button', { name: '保存' }).click()
  await expect(dialog(page)).toBeHidden()

  // 重新打开，勾选状态应该回填
  await page.getByRole('row').filter({ hasText: roleCode }).getByRole('button', { name: '授权' }).click()
  const reopened = dialog(page)
  await expect(reopened.getByRole('checkbox', { name: '查询用户' })).toBeChecked()
  await expect(reopened.getByRole('checkbox', { name: '删除用户' })).not.toBeChecked()
})

test('内置管理员的授权入口是空的，界面要说明原因', async ({ page }) => {
  await openPage(page, '角色管理')

  const row = page.getByRole('row').filter({ hasText: 'admin' }).first()
  // 内置角色的权限是隐式放行的，列表上要有标记，
  // 否则会有人去点授权、看到空勾选、以为权限丢了
  await expect(row.getByText('内置').first()).toBeVisible()
})

// API 权限页面是只读的：能看「权限码 ↔ 接口」，但没有任何新增/编辑入口。
test('API 权限页面展示权限码与它保护的接口', async ({ page }) => {
  await openPage(page, 'API 权限')

  // 后端由路由表生成这层对应关系，前端只负责渲染
  await expect(page.getByText('system:user:list', { exact: true })).toBeVisible()
  // exact 是必须的：getByText 默认子串匹配，'GET /api/v1/users' 会同时命中
  // 'GET /api/v1/users/:id' 那个徽标
  await expect(page.getByText('GET /api/v1/users', { exact: true })).toBeVisible()
  await expect(page.getByText('system:user:resetPwd', { exact: true })).toBeVisible()
  await expect(page.getByText('PUT /api/v1/users/:id/password', { exact: true })).toBeVisible()

  // 只读页不该有新增按钮——权限点由代码声明，界面上造不出来
  await expect(page.getByRole('button', { name: '新增' })).toBeHidden()

  // 文案来自前端字典，漏登记会显示原始键名
  await expect(page.getByText(/^perm\./)).toBeHidden()
  await expect(page.getByText('查询用户')).toBeVisible()
})
