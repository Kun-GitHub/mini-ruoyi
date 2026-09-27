import { api, setCsrfToken, setUnauthorizedHandler } from '$lib/api/client'
import type { MenuNode, SessionPayload, User } from '$lib/api/types'
import { resetTabs } from '$lib/stores/tabs.svelte'

type Phase = 'booting' | 'anonymous' | 'authenticated'

let phase = $state<Phase>('booting')
let user = $state<User | null>(null)
let isAdmin = $state(false)
let perms = $state<Set<string>>(new Set())
let menus = $state<MenuNode[]>([])

/**
 * 会话状态。
 *
 * 对外只暴露 getter：模块级 $state 不能直接导出可变绑定，
 * 而且这样能保证状态只能被本模块的 apply/clear 改动。
 */
export const session = {
  get phase(): Phase {
    return phase
  },
  get user(): User | null {
    return user
  },
  get isAdmin(): boolean {
    return isAdmin
  },
  get menus(): MenuNode[] {
    return menus
  },
  /**
   * 权限判断。
   *
   * 内置管理员的 perms 里会被后端铺满全部权限码，所以这里不需要
   * 再单独写 `isAdmin ||` 分支——只有一种判断路径，少一处漏判的可能。
   */
  can(code: string): boolean {
    return perms.has(code)
  },
}

function apply(payload: SessionPayload): void {
  user = payload.user
  isAdmin = payload.is_admin
  // 整体替换而不是原地修改：Set 的增删不是深层响应式的，替换引用才能触发更新
  perms = new Set(payload.perms)
  menus = payload.menus
  setCsrfToken(payload.csrf_token)
  phase = 'authenticated'
}

function clear(): void {
  user = null
  isAdmin = false
  perms = new Set()
  menus = []
  setCsrfToken(null)
  // 标签属于上一个会话：不清的话换个账号登录会看到别人的标签
  resetTabs()
  phase = 'anonymous'
}

export async function login(username: string, password: string): Promise<void> {
  apply(await api.post<SessionPayload>('/auth/login', { username, password }))
}

export async function logout(): Promise<void> {
  try {
    await api.post('/auth/logout')
  } finally {
    // 即使请求失败也要清本地状态：服务端会话可能已经没了，
    // 留着状态只会让用户停在「看起来已登录但什么都做不了」的界面
    clear()
  }
}

/**
 * 用 cookie 恢复会话。
 *
 * 刷新页面后 cookie 还在，但 CSRF 令牌只在内存里，所以必须重新问一次 /auth/me。
 */
export async function bootstrap(): Promise<void> {
  try {
    apply(await api.get<SessionPayload>('/auth/me'))
  } catch {
    clear()
  }
}

// 任何请求收到 401 都视为会话失效，立即清状态回到登录页。
// 注册在这里而不是 client 里，是为了让 client 保持对业务状态无感知。
setUnauthorizedHandler(clear)
