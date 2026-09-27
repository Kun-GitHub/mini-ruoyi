<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api } from '$lib/api/client'
  import type { FileItem, FilePage } from '$lib/api/types'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '$lib/components/ui/table'
  import { t } from '$lib/i18n/index.svelte'
  import { replaceQuery, route } from '$lib/router.svelte'
  import { confirm } from '$lib/stores/confirm.svelte'
  import { notifyError, notifySuccess } from '$lib/stores/notify.svelte'
  import { session } from '$lib/stores/session.svelte'

  const PAGE_SIZE = 20

  /** 与其它列表页一致：分页与筛选是本地状态，挂载时从 URL 读一次。 */
  const initial = new URLSearchParams(route.search)
  let page = $state(Math.max(1, Number(initial.get('page') ?? 1) || 1))
  let filters = $state({
    name: initial.get('name') ?? '',
    group: initial.get('group') ?? '',
  })

  let rows = $state<FileItem[]>([])
  let total = $state(0)
  let usage = $state({ used: 0, quota: 0, count: 0, max_size: 0 })
  let loading = $state(false)

  onMount(() => {
    void load()
  })

  async function load() {
    loading = true
    try {
      const q = new URLSearchParams()
      q.set('page', String(page))
      q.set('page_size', String(PAGE_SIZE))
      if (filters.name) q.set('name', filters.name)
      if (filters.group) q.set('group', filters.group)

      // 用量随列表一起返回，省掉一次往返
      const res = await api.get<FilePage>(`/files?${q}`)
      rows = res.list
      total = res.total
      usage = res.usage
      if (res.page !== page) {
        page = res.page
        syncURL()
      }
    } catch (err) {
      notifyError(err instanceof ApiError ? err.key : 'error.internal')
    } finally {
      loading = false
    }
  }

  function syncURL() {
    replaceQuery({ ...filters, page: page === 1 ? undefined : page })
  }

  function search() {
    page = 1
    syncURL()
    void load()
  }

  function resetFilters() {
    filters = { name: '', group: '' }
    page = 1
    syncURL()
    void load()
  }

  function goPage(target: number) {
    page = target
    syncURL()
    void load()
  }

  // ---- 上传 ----

  let uploading = $state(false)
  let uploadGroup = $state('')

  async function upload(event: Event) {
    // 先把元素抓下来：await 之后 event.currentTarget 可能已经被置空
    const input = event.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return

    uploading = true
    try {
      const form = new FormData()
      form.append('file', file)
      if (uploadGroup) form.append('group', uploadGroup)

      await api.upload<FileItem>('/files', form)
      notifySuccess('notify.uploaded')
      // 上传后回到第一页，新文件按 id 倒序就在最前面
      page = 1
      syncURL()
      await load()
    } catch (err) {
      notifyError(err instanceof ApiError ? err.key : 'error.internal')
    } finally {
      uploading = false
      // 清空选择，否则同一个文件第二次选不会触发 change
      input.value = ''
    }
  }

  // ---- 删除 ----

  async function remove(item: FileItem) {
    const ok = await confirm({
      titleKey: 'confirm.deleteTitle',
      bodyKey: 'file.deleteConfirm',
      details: [
        { labelKey: 'file.name', value: item.original_name },
        { labelKey: 'file.size', value: formatSize(item.size) },
      ],
      danger: true,
    })
    if (!ok) return

    try {
      await api.del(`/files/${item.id}`)
      notifySuccess('notify.deleted')
      if (rows.length === 1 && page > 1) goPage(page - 1)
      else await load()
    } catch (err) {
      notifyError(err instanceof ApiError ? err.key : 'error.internal')
    }
  }

  // ---- 展示辅助 ----

  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
    return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
  }

  const totalPages = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)))
</script>

<div class="flex flex-col gap-4">
  <div class="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
    <p>{t('file.hint')}</p>
    <p class="mt-2">
      {t('file.usage', {
        used: formatSize(usage.used),
        quota: formatSize(usage.quota),
        count: usage.count,
      })}
    </p>
  </div>

  <div data-testid="filters" class="rounded-lg border bg-card p-4">
    <div class="flex flex-wrap items-end gap-3">
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('file.name')}</span>
        <Input bind:value={filters.name} onkeydown={(e) => e.key === 'Enter' && search()} />
      </label>
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('file.group')}</span>
        <Input bind:value={filters.group} onkeydown={(e) => e.key === 'Enter' && search()} />
      </label>

      <div class="flex gap-2">
        <Button variant="outline" onclick={resetFilters}>{t('common.reset')}</Button>
        <Button onclick={search}>{t('common.search')}</Button>
      </div>
    </div>
  </div>

  {#if session.can('tool:file:upload')}
    <div class="flex flex-wrap items-end gap-3 rounded-lg border bg-card p-4">
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('file.group')}</span>
        <Input bind:value={uploadGroup} maxlength={64} />
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('file.upload')}</span>
        <!-- 用 Input 组件而不是裸 input：它自带 type="file" 分支，
             高度与其它控件一致（手写 h-x 就会和别处跑偏） -->
        <Input
          data-testid="file-input"
          type="file"
          class="w-72 cursor-pointer"
          disabled={uploading}
          onchange={upload}
        />
      </label>
      <!-- 单文件上限用后端给的真实值，不在前端写常量 -->
      <p class="text-xs text-muted-foreground">
        {t('file.uploadHint', { size: formatSize(usage.max_size) })}
      </p>
    </div>
  {/if}

  <p class="text-sm text-muted-foreground">{t('common.total', { total })}</p>

  <div class="rounded-lg border bg-card">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('file.name')}</TableHead>
          <TableHead>{t('file.group')}</TableHead>
          <TableHead class="text-right">{t('file.size')}</TableHead>
          <TableHead>{t('file.uploader')}</TableHead>
          <TableHead>{t('file.uploadedAt')}</TableHead>
          <TableHead class="text-right">{t('common.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#if rows.length === 0}
          <TableRow>
            <TableCell colspan={6} class="py-10 text-center text-muted-foreground">
              {loading ? t('common.loading') : t('common.empty')}
            </TableCell>
          </TableRow>
        {/if}
        {#each rows as item (item.id)}
          <TableRow>
            <TableCell class="max-w-72 truncate font-medium" title={item.original_name}>
              {item.original_name}
            </TableCell>
            <TableCell class="text-muted-foreground">{item.group_name || '—'}</TableCell>
            <TableCell class="text-right tabular-nums">{formatSize(item.size)}</TableCell>
            <TableCell class="text-muted-foreground">{item.uploader_name || '—'}</TableCell>
            <TableCell class="text-muted-foreground">
              {new Date(item.created_at).toLocaleString()}
            </TableCell>
            <TableCell class="text-right whitespace-nowrap">
              {#if session.can('tool:file:list')}
                <!-- 直接给一个链接：同源请求浏览器会自动带上会话 cookie，
                     不需要 fetch + blob 那套。响应是 attachment，点击即下载。 -->
                <a
                  href="/api/v1/files/{item.id}/download"
                  class="inline-flex h-6 items-center rounded-md px-2 text-xs transition-colors hover:bg-accent"
                >
                  {t('file.download')}
                </a>
              {/if}
              {#if session.can('tool:file:delete')}
                <Button variant="ghost" size="sm" class="text-destructive" onclick={() => remove(item)}>
                  {t('common.delete')}
                </Button>
              {/if}
            </TableCell>
          </TableRow>
        {/each}
      </TableBody>
    </Table>
  </div>

  {#if totalPages > 1}
    <div class="flex items-center justify-end gap-2 text-sm">
      <Button variant="outline" size="sm" disabled={page <= 1} onclick={() => goPage(page - 1)}>‹</Button>
      <span class="text-muted-foreground tabular-nums">{page} / {totalPages}</span>
      <Button variant="outline" size="sm" disabled={page >= totalPages} onclick={() => goPage(page + 1)}>›</Button>
    </div>
  {/if}
</div>
