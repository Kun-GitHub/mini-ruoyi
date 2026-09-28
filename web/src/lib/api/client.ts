/** 后端响应体的字段级校验错误，对应 internal/httpx.FieldError。 */
export type FieldError = {
  field: string
  rule: string
  param?: string
}

/**
 * 后端响应的信封，对应 internal/httpx.Response。
 *
 * `code` 恒等于 HTTP 状态码（后端由 httpserver 的 write() 统一写入）。
 * 它的作用不是「判断成功与否」——那是 res.ok 的活——而是**证明这份 body 属于本服务**：
 * 拿到一个 200 但 body 不是信封，就说明请求被代理/网关拦了，或者后端压根没起来。
 */
type Envelope = {
  code: number
  msg: string
  data?: unknown
  errors?: FieldError[]
}

/**
 * 所有非成功响应的统一错误。
 *
 * key 是后端的 i18n 键（如 error.notFound），展示时用 t(key) 翻译。
 * 后端只出键、前端出文案，所以这里存的永远是键而不是已经翻好的句子。
 */
export class ApiError extends Error {
  readonly status: number
  readonly key: string
  readonly fieldErrors: FieldError[]
  /** 只有 409 + error.hasDependents 时非空，形如 { child_menus: 3 }。 */
  readonly dependents: Record<string, number> | null

  constructor(
    status: number,
    key: string,
    fieldErrors: FieldError[] = [],
    dependents: Record<string, number> | null = null,
  ) {
    super(`${status} ${key}`)
    this.name = 'ApiError'
    this.status = status
    this.key = key
    this.fieldErrors = fieldErrors
    this.dependents = dependents
  }

  /**
   * 是否为「删除需要确认」的信号。
   *
   * ⚠️ 这类响应**不是错误**：后端在删除有子数据的资源时返回 409 + 影响面，
   * 意图是让前端弹确认框而不是报错。调用方必须先判这个再决定怎么处理，
   * 否则用户会先看到一个红色报错、再看到确认框。
   */
  get needsConfirmation(): boolean {
    return this.key === 'error.hasDependents'
  }

  /** 取某个字段的错误，用于把提示挂到对应输入框上。 */
  fieldError(field: string): FieldError | undefined {
    return this.fieldErrors.find((e) => e.field === field)
  }
}

let csrfToken: string | null = null

/** 由会话状态在登录/登出/刷新恢复时调用。 */
export function setCsrfToken(token: string | null): void {
  csrfToken = token
}

let unauthorizedHandler: (() => void) | null = null

/** 会话失效时的回调，由会话状态注册成「清空状态并回登录页」。 */
export function setUnauthorizedHandler(handler: () => void): void {
  unauthorizedHandler = handler
}

function extractDependents(data: unknown): Record<string, number> | null {
  if (!data || typeof data !== 'object') return null
  const out: Record<string, number> = {}
  for (const [k, v] of Object.entries(data)) {
    if (typeof v === 'number') out[k] = v
  }
  return Object.keys(out).length > 0 ? out : null
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  // 写操作必须带 CSRF 令牌。GET/HEAD 不需要——CSRF 防护针对的是「改变状态」。
  if (csrfToken && method !== 'GET' && method !== 'HEAD') {
    headers['X-CSRF-Token'] = csrfToken
  }

  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    // fetch 本身抛异常 = 网络层失败，此时没有 HTTP 状态码可用
    throw new ApiError(0, 'error.network')
  }

  const text = await res.text()
  let envelope: Envelope | null = null
  if (text) {
    try {
      envelope = JSON.parse(text) as Envelope
    } catch {
      envelope = null
    }
  }

  /**
   * 不是本服务的信封格式，说明请求没走到我们的 handler。
   *
   * 实测：Vite dev server 在后端没启动时代理会返回 502 + text/plain + 空 body。
   * 这种情况**不能报 error.internal** ——那会让人去翻后端日志，而真正的排查方向
   * 是「后端进程在不在」。反过来，只要响应来自本服务，它一定是信封格式
   * （连 panic 也由自定义 Recovery 写成信封），所以这个判断是可靠的。
   */
  if (!envelope || typeof envelope.code !== 'number') {
    const contentType = res.headers.get('content-type') ?? '未知'
    console.error(
      `[api] ${method} ${path} 的响应不是本服务的信封格式。` +
        `HTTP ${res.status}，Content-Type: ${contentType}。` +
        `常见原因：后端进程未启动、反向代理配置错误。响应体：`,
      text.slice(0, 200) || '(空)',
    )
    throw new ApiError(res.status, 'error.backendUnreachable')
  }

  if (res.ok) {
    return envelope.data as T
  }

  if (res.status === 401) {
    // 会话失效：清掉本地状态，避免后续请求继续带着一个无效 cookie 空跑
    unauthorizedHandler?.()
  }
  throw new ApiError(
    res.status,
    envelope?.msg ?? 'error.internal',
    envelope?.errors ?? [],
    extractDependents(envelope?.data),
  )
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),

  /**
   * 上传 multipart 表单。
   *
   * 单独一个方法而不是复用 request()：**不能设置 Content-Type**——
   * multipart 的 Content-Type 里带 boundary，必须由浏览器生成。
   * 手写一个 application/json 会让后端完全解析不出文件。
   */
  upload: <T>(path: string, form: FormData) => requestForm<T>('POST', path, form),
}

async function requestForm<T>(method: string, path: string, form: FormData): Promise<T> {
  const headers: Record<string, string> = {}
  if (csrfToken) headers['X-CSRF-Token'] = csrfToken

  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, { method, headers, body: form })
  } catch {
    throw new ApiError(0, 'error.network')
  }

  const text = await res.text()
  let envelope: Envelope | null = null
  if (text) {
    try {
      envelope = JSON.parse(text) as Envelope
    } catch {
      envelope = null
    }
  }

  if (!envelope || typeof envelope.code !== 'number') {
    console.error(
      `[api] ${method} ${path} 的响应不是本服务的信封格式。HTTP ${res.status}。响应体：`,
      text.slice(0, 200) || '(空)',
    )
    throw new ApiError(res.status, 'error.backendUnreachable')
  }
  if (res.ok) return envelope.data as T

  if (res.status === 401) unauthorizedHandler?.()
  throw new ApiError(
    res.status,
    envelope.msg,
    envelope.errors ?? [],
    extractDependents(envelope.data),
  )
}

/**
 * 按分页参数拼查询串。只带非默认值，让 URL 保持干净。
 */
export function pageQuery(page: number, pageSize: number): string {
  const params = new URLSearchParams()
  if (page !== 1) params.set('page', String(page))
  if (pageSize !== 20) params.set('page_size', String(pageSize))
  const s = params.toString()
  return s ? `?${s}` : ''
}
