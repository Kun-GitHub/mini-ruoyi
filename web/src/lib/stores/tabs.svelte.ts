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

let tabs = $state<Tab[]>([])

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
  tabs = [...tabs, tab]
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
  tabs = next

  // 优先接替右边的标签，没有就接左边——和浏览器的行为一致
  return next[index]?.path ?? next[index - 1]?.path ?? null
}

export function closeOthers(path: string): void {
  tabs = tabs.filter((t) => t.path === path)
}

/** 关闭全部，返回剩下的数量（调用方据此决定是否要清空内容区）。 */
export function closeAll(): void {
  tabs = []
}

/** 登出时调用。不清理的话，换个账号登录会看到上一个账号的标签。 */
export function resetTabs(): void {
  tabs = []
}
