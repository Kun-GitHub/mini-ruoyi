import type { Component } from 'svelte'

import type { MenuNode } from '$lib/api/types'

/**
 * 页面模块表。
 *
 * 用 eager 一次性收进来，路由解析就是同步的：不用 await、不用加载态、
 * 不会出现「点了菜单先闪一下空白」。页面数量是十几个量级时这个取舍划算；
 * 真涨到几十个再改成懒加载（去掉 eager，配合 {#await} 渲染）。
 *
 * 键形如 ../pages/system/users.svelte，对应菜单里的 component = "system/users"。
 */
const modules = import.meta.glob('../pages/**/*.svelte', { eager: true }) as Record<
  string,
  { default: Component }
>

export type FlatMenu = {
  path: string
  titleKey: string
  component: string
  icon: string
}

/** 把菜单树压平成一维，只保留有页面组件的项（目录本身不可导航）。 */
export function flattenMenus(nodes: MenuNode[]): FlatMenu[] {
  const out: FlatMenu[] = []
  const walk = (list: MenuNode[]): void => {
    for (const node of list) {
      if (node.menu_type === 'menu' && node.path && node.component) {
        out.push({
          path: node.path,
          titleKey: node.title_key,
          component: node.component,
          icon: node.icon,
        })
      }
      walk(node.children)
    }
  }
  walk(nodes)
  return out
}

/**
 * 解析页面组件。
 *
 * 找不到时返回 null 而不是抛错：菜单是数据库里的数据，可能指向一个
 * 尚未实现或已被删掉的前端页面。这时应该显示一个明确的提示，
 * 而不是让整个应用崩掉——那会让用户连导航都点不了。
 */
export function resolveComponent(component: string): Component | null {
  return modules[`../pages/${component}.svelte`]?.default ?? null
}

// ---- 极简路由 ----

let pathname = $state(window.location.pathname)
let search = $state(window.location.search)

window.addEventListener('popstate', () => {
  pathname = window.location.pathname
  search = window.location.search
})

function commit(to: string, replace: boolean): void {
  const url = new URL(to, window.location.origin)
  if (url.pathname + url.search === pathname + search) return
  if (replace) {
    history.replaceState(null, '', to)
  } else {
    history.pushState(null, '', to)
  }
  pathname = window.location.pathname
  search = window.location.search
}

export const route = {
  get pathname(): string {
    return pathname
  },
  get search(): string {
    return search
  },
}

export function navigate(to: string): void {
  commit(to, false)
}

/** 用在「登录后跳转」「重定向到登录页」这类不希望留下历史记录的场景。 */
export function replaceTo(to: string): void {
  commit(to, true)
}

/**
 * 只替换当前页面的查询串，不留历史记录。
 *
 * 列表页改筛选/翻页时用：这些操作很密集，用 push 会把浏览器历史塞满，
 * 用户按一次后退还在同一个页面上，体验很差。
 */
export function replaceQuery(params: Record<string, string | number | undefined>): void {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') q.set(k, String(v))
  }
  const qs = q.toString()
  commit(qs ? `${pathname}?${qs}` : pathname, true)
}
