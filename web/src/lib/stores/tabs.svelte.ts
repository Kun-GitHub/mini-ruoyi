/* 有问题欢迎联系 hot_kun@hotmail.com */

/** 多标签页。
 *
 * 只维护「打开了哪些标签」，**当前激活的是哪一个由路由决定**（`route.pathname`）。
 * 不在这里另存一份 activePath，否则两处状态会互相打架——
 * 浏览器前进/后退、直接改地址栏都会绕过 store。
 */

export type Tab = {
  path: string
  titleKey: string
  component: string
}

/**
 * 标签持久化的存储键。
 *
 * 用 sessionStorage 而非 localStorage：标签属于**这一次**浏览器会话，
 * 关掉标签页就该忘掉。用 localStorage 会变成「上周开过的页面今天还开着」，
 * 也会和「登出即清标签」的语义打架。
 */
const STORAGE_KEY = 'mr.tabs'

/**
 * 从 sessionStorage 读回标签。
 *
 * 这里必须做**形状校验**，不能直接 JSON.parse 完就当 Tab[] 用：
 * 存储里的内容可能来自旧版本（字段改名）或被人手改过，
 * 而 app-shell 会拿 tab.component / tab.titleKey 直接去渲染——
 * 一个 undefined 就能让整个应用白屏，而用户只会看到「刷新后打不开了」。
 * 校验不过就当空数组：宁可丢标签，也不能让应用起不来。
 */
function load(): Tab[] {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter(
      (t): t is Tab =>
        typeof t === 'object' &&
        t !== null &&
        typeof (t as Tab).path === 'string' &&
        typeof (t as Tab).titleKey === 'string' &&
        typeof (t as Tab).component === 'string' &&
        (t as Tab).path !== '',
    )
  } catch {
    // 存储不可用（隐私模式）或内容不是 JSON：都当作「没有持久化」，
    // 标签功能退化成刷新即丢，而不是抛错把应用卡住
    return []
  }
}

function save(next: Tab[]): void {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  } catch {
    // 写不进去（配额满 / 隐私模式）不影响本次操作，只是这一轮不会被记住
  }
}

/** 统一从这里改 tabs：替换引用触达响应式，同时落盘，两件事不会漏掉一件。 */
function commit(next: Tab[]): void {
  tabs = next
  save(next)
}

let tabs = $state<Tab[]>(load())

export const tabState = {
  get items(): Tab[] {
    return tabs
  },
}

export function isOpen(path: string): boolean {
  return tabs.some((t) => t.path === path)
}

/** 打开（或激活）一个标签。重复打开同一个路径是幂等的。 */
export function openTab(tab: Tab): void {
  if (tab.path === '' || isOpen(tab.path)) return
  commit([...tabs, tab])
}

/**
 * 关闭一个标签，返回应当跳转到的路径。
 *
 * 返回 null 表示「没有别的标签了」，由调用方决定去哪（外壳会显示一个空态）。
 * 只关掉一个非激活标签时调用方不该跳转，所以这里把判断留给调用方。
 */
export function closeTab(path: string): string | null {
  const index = tabs.findIndex((t) => t.path === path)
  if (index < 0) return null

  const next = [...tabs]
  next.splice(index, 1)
  commit(next)

  // 优先接替右边的标签，没有就接左边——和浏览器的行为一致
  return next[index]?.path ?? next[index - 1]?.path ?? null
}

export function closeOthers(path: string): void {
  commit(tabs.filter((t) => t.path === path))
}

/** 关闭全部。 */
export function closeAll(): void {
  commit([])
}

/** 登出时调用。不清理的话，换个账号登录会看到上一个账号的标签。 */
export function resetTabs(): void {
  // 持久化的那份也要清：不清的话刷新一次，上一个账号的标签又回来了
  commit([])
}
