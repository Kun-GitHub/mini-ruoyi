/** 通知（toast）。
 *
 * 存的是 i18n **键**而不是翻好的句子，翻译交给渲染层——
 * 这样切换语言时已经弹出的通知会跟着变，也和后端「只出键」的约定一致。
 */

export type NoticeKind = 'error' | 'success'

export type Notice = {
  id: number
  kind: NoticeKind
  key: string
  params?: Record<string, string | number>
}

let items = $state<Notice[]>([])
let nextID = 1

const AUTO_DISMISS_MS = 5000

export const notices = {
  get items(): Notice[] {
    return items
  },
}

function push(kind: NoticeKind, key: string, params?: Record<string, string | number>): void {
  const id = nextID++
  items = [...items, { id, kind, key, params }]
  setTimeout(() => dismiss(id), AUTO_DISMISS_MS)
}

export function notifyError(key: string, params?: Record<string, string | number>): void {
  push('error', key, params)
}

export function notifySuccess(key: string, params?: Record<string, string | number>): void {
  push('success', key, params)
}

export function dismiss(id: number): void {
  items = items.filter((n) => n.id !== id)
}
