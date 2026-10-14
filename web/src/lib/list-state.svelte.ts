// 有问题欢迎联系 hot_kun@hotmail.com
import { ApiError, api } from '$lib/api/client'
import type { Page } from '$lib/api/types'
import type { MessageKey } from '$lib/i18n/index.svelte'
import { replaceQuery, route } from '$lib/router.svelte'
import { notifyError } from '$lib/stores/notify.svelte'

/** 筛选条件的值一律是字符串：URL 与查询参数本来就是字符串，类型转换由各页自己决定。 */
export type ListFilters<K extends string> = Record<K, string>

export type ListOptions<T, K extends string, R extends Page<T> = Page<T>> = {
  /** 列表接口路径，不含 `/api/v1` 前缀与查询串（例如 `/users`）。 */
  endpoint: string
  /** 参与查询串与地址栏的筛选字段；也是 reset 要清空的字段。没有筛选就给空数组。 */
  filterKeys: readonly K[]
  pageSize?: number
  /** 给了才有 exportHref；要带前缀（`/api/v1/...`），它直接给 `<a href>` 用。 */
  exportPath?: string
  /** 响应里除 list/total/page 之外还要读的东西（如文件页的用量），由页面自己接。 */
  onResponse?: (res: R) => void
}

export type ListState<T, K extends string, R extends Page<T> = Page<T>> = {
  readonly page: number
  readonly filters: ListFilters<K>
  readonly rows: T[]
  readonly total: number
  readonly loading: boolean
  readonly totalPages: number
  readonly exportHref: string
  load(): Promise<void>
  /** 回到第一页并按当前筛选重新加载——「搜索」与「写操作后刷新」是同一件事。 */
  search(): void
  /** 清空筛选、回第一页、重新加载。 */
  reset(): void
  goPage(target: number): void
  /** 删除后的刷新：删掉的是本页最后一条就回退一页，否则原地刷新。 */
  refreshAfterDelete(): Promise<void>
}

/**
 * 列表页的公共状态机：分页、筛选、加载、导出链接、URL 同步。
 *
 * 六个列表页（用户/角色/文件/会话/登录日志/操作日志）原本各自手写同一套
 * 约 80 行的逻辑，改一处要改六处；这里收成一处，页面只剩字段与列。
 */
export function createListState<T, K extends string, R extends Page<T> = Page<T>>(
  opts: ListOptions<T, K, R>,
): ListState<T, K, R> {
  const { endpoint, filterKeys, pageSize = 20, exportPath, onResponse } = opts

  /**
   * 分页与筛选是**组件本地状态**，只在挂载（即本函数执行）时从 URL 读一次。
   *
   * 为什么不持续跟随 URL：多标签页会把打开过的页面同时渲染，而 URL 只反映当前
   * 激活的那个标签。隐藏的页面若持续读全局 URL，切换标签会把它自己的筛选条件
   * 冲掉，还会让它拿着别人的参数重新发请求。
   *
   * 代价是浏览器前进/后退不会回填筛选框。刷新仍然有效——挂载时读一次 URL。
   */
  const initial = new URLSearchParams(route.search)
  const state = $state({
    page: Math.max(1, Number(initial.get('page') ?? 1) || 1),
    filters: Object.fromEntries(filterKeys.map((k) => [k, initial.get(k) ?? ''])) as ListFilters<K>,
    rows: [] as T[],
    total: 0,
    loading: false,
  })

  async function load() {
    state.loading = true
    try {
      const q = new URLSearchParams()
      q.set('page', String(state.page))
      q.set('page_size', String(pageSize))
      for (const key of filterKeys) {
        const v = state.filters[key]
        if (v) q.set(key, v)
      }
      const res = await api.get<R>(`${endpoint}?${q}`)
      state.rows = res.list
      state.total = res.total
      onResponse?.(res)
      // 服务端会把越界的 page 钳到最后一页，以它为准。
      // 自己留着 page=99 的话，分页器会显示 "99 / 2" 这种不可能的状态
      if (res.page !== state.page) {
        state.page = res.page
        syncURL()
      }
    } catch (err) {
      notifyError(errKey(err))
    } finally {
      state.loading = false
    }
  }

  /** 把当前状态反映到地址栏，方便刷新与分享。方向是单向的：只写不读。 */
  function syncURL() {
    replaceQuery({ ...state.filters, page: state.page === 1 ? undefined : state.page })
  }

  function search() {
    state.page = 1
    syncURL()
    void load()
  }

  function reset() {
    state.filters = Object.fromEntries(filterKeys.map((k) => [k, ''])) as ListFilters<K>
    state.page = 1
    syncURL()
    void load()
  }

  function goPage(target: number) {
    state.page = target
    syncURL()
    void load()
  }

  async function refreshAfterDelete() {
    if (state.rows.length === 1 && state.page > 1) goPage(state.page - 1)
    else await load()
  }

  return {
    get page() {
      return state.page
    },
    get filters() {
      return state.filters
    },
    get rows() {
      return state.rows
    },
    get total() {
      return state.total
    },
    get loading() {
      return state.loading
    },
    get totalPages() {
      return Math.max(1, Math.ceil(state.total / pageSize))
    },
    /**
     * 导出链接给 `<a href>` 用（Button 的 href 分支），而不是按钮 + onclick：
     * 下载是浏览器的行为，交给它处理进度与保存位置。筛选条件必须带上——
     * 导出的应该是屏幕上看到的那些行，不是全表。
     */
    get exportHref() {
      if (!exportPath) return ''
      const q = new URLSearchParams()
      for (const key of filterKeys) {
        const v = state.filters[key]
        if (v) q.set(key, v)
      }
      const qs = q.toString()
      return `${exportPath}${qs ? `?${qs}` : ''}`
    },
    load,
    search,
    reset,
    goPage,
    refreshAfterDelete,
  }
}

/**
 * 后端错误 → 通知用的文案键。
 *
 * 后端的键是字符串，这里收窄成 MessageKey：它最后总是喂给 t() 或 notifyError()，
 * 收窄一次比在每个调用点 as 一次要好。
 */
export function errKey(err: unknown): MessageKey {
  return err instanceof ApiError ? (err.key as MessageKey) : 'error.internal'
}
