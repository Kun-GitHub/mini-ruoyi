/** 与后端 DTO 一一对应的类型。字段名保持后端的 snake_case，不做本地改名。 */

export type { FieldError } from './client'

export type Status = 'active' | 'inactive'
export type MenuType = 'directory' | 'menu'

export type User = {
  id: number
  created_at: string
  updated_at: string
  status: Status
  username: string
  nickname: string
  mobile: string
  email: string
  login_ip: string
  /** null 表示从未登录 */
  login_at: string | null
}

export type UserDetail = User & {
  role_ids: number[]
}

export type Role = {
  id: number
  created_at: string
  updated_at: string
  status: Status
  code: string
  name: string
  remark: string
}

export type Grants = {
  menu_ids: number[]
  perm_codes: string[]
}

export type Menu = {
  id: number
  created_at: string
  updated_at: string
  status: Status
  /** null 表示根节点 */
  parent_id: number | null
  sort: number
  menu_type: MenuType
  /** i18n 键，不是文案 */
  title_key: string
  path: string
  component: string
  icon: string
}

export type MenuNode = Menu & {
  children: MenuNode[]
}

export type Page<T> = {
  list: T[]
  total: number
  page: number
  page_size: number
}

/** 登录与 /auth/me 的响应。 */
export type SessionPayload = {
  user: User
  is_admin: boolean
  /** 内置管理员这里会拿到全部已声明的权限码，所以前端只需一种判断 */
  perms: string[]
  csrf_token: string
  expires_at: string
  menus: MenuNode[]
}

export type SessionView = {
  /** 用于踢人的标识。它是明文 token 的 SHA-256，反推不出 token。 */
  token_hash: string
  user_id: number
  username: string
  nickname: string
  created_at: string
  expires_at: string
  last_seen_at: string
  ip: string
  user_agent: string
}

export type LoginLog = {
  id: number
  created_at: string
  username: string
  status: 'success' | 'failed'
  /** 失败原因的 i18n 键 */
  reason: string
  ip: string
  user_agent: string
}

export type OperLog = {
  id: number
  created_at: string
  user_id: number
  username: string
  method: string
  path: string
  status: number
  /** 失败时的 i18n 键 */
  result: string
  duration_ms: number
  ip: string
  user_agent: string
}

export type FileItem = {
  id: number
  created_at: string
  group_name: string
  original_name: string
  size: number
  content_type: string
  uploader_id: number
  uploader_name: string
}

export type FileUsage = {
  used: number
  quota: number
  count: number
  /** 单文件上限，由后端配置决定 */
  max_size: number
}

export type FilePage = Page<FileItem> & {
  usage: FileUsage
}

export type SystemSnapshot = {
  os: string
  arch: string
  go_version: string
  uptime_seconds: number
  cpu: {
    /** false 表示本平台读不到（比如 macOS 没有 /proc） */
    available: boolean
    cores: number
    usage_percent: number
    load_avg: number[] | null
    load_available: boolean
  }
  memory: {
    available: boolean
    total: number
    used: number
    free: number
    usage_percent: number
  }
  disk: {
    path: string
    available: boolean
    total: number
    used: number
    free: number
    usage_percent: number
  }
  process: {
    goroutines: number
    heap_alloc: number
    heap_sys: number
    sys: number
    num_gc: number
    rss_available: boolean
    rss: number
  }
}

export type Job = {
  status: 'active' | 'inactive'
  job_key: string
  cron: string
  remark: string
  /** 说明文案的 i18n 键，来自代码注册表 */
  description_key: string
  /** 代码里注册的默认 cron，用于「恢复默认」时参考 */
  default_cron: string
  last_run_at: string | null
  /** 空串表示从未执行过 */
  last_status: '' | 'success' | 'failed' | 'skipped'
  last_error: string
  last_duration_ms: number
  next_run_at: string | null
}

export type JobLog = {
  id: number
  /** 执行开始时间，与任务列表上的「上次执行」同源 */
  created_at: string
  job_key: string
  /** cron = 调度器触发，manual = 界面上点了「立即执行」 */
  trigger: 'cron' | 'manual'
  status: 'success' | 'failed' | 'skipped'
  /** 失败原因或跳过原因，成功时为空 */
  error: string
  duration_ms: number
}

export type PermEndpoint = {
  method: string
  path: string
  /** 与 endpoints 同级冗余一份，便于前端按需索引 */
  perm: string
}

export type PermItem = {
  code: string
  label_key: string
  /** 该权限点保护的接口。后端由路由表生成，不查库。 */
  endpoints: PermEndpoint[]
}

export type PermGroup = {
  title_key: string
  perms: PermItem[]
}

export type PermCatalogue = {
  groups: PermGroup[]
}
