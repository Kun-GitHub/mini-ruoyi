/** 确认框。
 *
 * 用 Promise 而不是「状态 + 回调」：调用方写下来就是线性的
 *
 *   if (!(await confirm({...}))) return
 *   await api.del(path)
 *
 * 而状态式写法需要把后续逻辑拆成回调，删除流程会散成好几段。
 */

export type ConfirmDetail = {
  /** i18n 键 */
  labelKey: string
  value: string | number
}

export type ConfirmRequest = {
  titleKey: string
  bodyKey?: string
  details?: ConfirmDetail[]
  /** 危险操作（删除）用红色确认按钮 */
  danger?: boolean
}

let current = $state<ConfirmRequest | null>(null)
let resolver: ((ok: boolean) => void) | null = null

export const confirmState = {
  get current(): ConfirmRequest | null {
    return current
  },
}

export function confirm(request: ConfirmRequest): Promise<boolean> {
  // 同一时刻只允许一个确认框。前一个若还挂着，视为取消，
  // 避免两个 Promise 的 resolver 互相覆盖导致某一方永远不 resolve。
  resolver?.(false)
  current = request
  return new Promise<boolean>((resolve) => {
    resolver = resolve
  })
}

export function resolveConfirm(ok: boolean): void {
  const resolve = resolver
  resolver = null
  current = null
  resolve?.(ok)
}
