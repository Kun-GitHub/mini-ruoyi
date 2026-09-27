import { expect, type Page } from '@playwright/test'

export const ADMIN = { username: 'admin', password: 'admin123' }

/**
 * 提交登录表单，不断言结果。
 *
 * 断言成功是 loginAsAdmin 的事——否则「故意用错密码」的用例
 * 会在这一步就挂掉，看起来像是登录页坏了。
 */
export async function submitLogin(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
}

/**
 * 退出登录，并等到界面真的切回登录表单。
 *
 * 不能点完就接着填表单：logout 是异步的，AppShell 会多留一小会儿，
 * 而那期间「上一个页面的筛选栏」可能也有同名输入框（用户列表就有「用户名」），
 * Playwright 会把值填进那个即将消失的元素，登录表单里还是空的——
 * HTML5 的 required 于是直接拦住提交，一个请求都不会发出去。
 * 表现是后面所有断言都失败，但看不出跟登出有什么关系。
 */
export async function logout(page: Page): Promise<void> {
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '退出登录' })).toBeHidden()
}

/** 用默认管理员登录，并等到应用外壳就绪。 */
export async function loginAsAdmin(page: Page): Promise<void> {
  await submitLogin(page, ADMIN.username, ADMIN.password)
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
}

/**
 * 侧边栏作用域。
 *
 * 标签栏和侧边栏的按钮文案完全相同，不限定作用域的话
 * `getByRole('button', { name })` 会同时命中两者，触发 Playwright 的 strict mode 报错。
 */
export function sidebar(page: Page) {
  return page.getByTestId('sidebar')
}

export function tabBar(page: Page) {
  return page.getByTestId('tab-bar')
}

/**
 * 当前可见的标签面板。
 *
 * 多标签页会把所有打开过的页面都留在 DOM 里（只隐藏），所以同一个页面元素
 * 可能同时存在多份——不限定作用域的话 getByTestId('filters') 会命中多个。
 */
export function activePanel(page: Page) {
  return page.locator('[data-testid="tab-panel"]:visible')
}

/**
 * 筛选栏作用域。
 *
 * 限定到可见面板，还要限定到筛选栏内部：新建弹窗里也有「用户名」这类标签，
 * 不限定会命中多个元素。
 */
export function filters(page: Page) {
  return activePanel(page).getByTestId('filters')
}

export function dialog(page: Page) {
  return page.getByRole('dialog')
}

export function toaster(page: Page) {
  return page.getByTestId('toaster')
}

/**
 * 断言出现某条通知。
 *
 * 用 .first()：通知是叠加显示的，连续两次保存后屏幕上会同时有两条「已保存」，
 * 不加会触发 strict mode。断言「出现了」就够了，不需要区分是哪一条。
 */
export async function expectToast(page: Page, text: string | RegExp): Promise<void> {
  await expect(toaster(page).getByText(text).first()).toBeVisible()
}

/** 从侧边栏进入某个页面，等到标签栏出现对应标签。 */
export async function openPage(page: Page, title: string): Promise<void> {
  await sidebar(page).getByRole('button', { name: title, exact: true }).click()
  await expect(tabBar(page).getByTestId('tab').filter({ hasText: title })).toBeVisible()
}

/** 生成唯一名字，避免测试之间因为数据残留互相干扰。 */
let seq = 0
export function unique(prefix: string): string {
  seq += 1
  return `${prefix}${Date.now().toString(36)}${seq}`
}
