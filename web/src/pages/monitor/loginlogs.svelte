<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api } from '$lib/api/client'
  import type { LoginLog, Page } from '$lib/api/types'
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { NativeSelect } from '$lib/components/ui/native-select'
  import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '$lib/components/ui/table'
  import { t, tKey } from '$lib/i18n/index.svelte'
  import { replaceQuery, route } from '$lib/router.svelte'
  import { notifyError } from '$lib/stores/notify.svelte'

  const PAGE_SIZE = 20

  /** 与其它列表页一致：分页与筛选是本地状态，挂载时从 URL 读一次。 */
  const initial = new URLSearchParams(route.search)
  let page = $state(Math.max(1, Number(initial.get('page') ?? 1) || 1))
  let filters = $state({
    username: initial.get('username') ?? '',
    status: initial.get('status') ?? '',
  })

  let rows = $state<LoginLog[]>([])
  let total = $state(0)
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
      if (filters.username) q.set('username', filters.username)
      if (filters.status) q.set('status', filters.status)

      const res = await api.get<Page<LoginLog>>(`/login-logs?${q}`)
      rows = res.list
      total = res.total
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
    filters = { username: '', status: '' }
    page = 1
    syncURL()
    void load()
  }

  function goPage(target: number) {
    page = target
    syncURL()
    void load()
  }

  const totalPages = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)))
</script>

<div class="flex flex-col gap-4">
  <div class="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
    <p>{t('loginlog.hint')}</p>
  </div>

  <div data-testid="filters" class="rounded-lg border bg-card p-4">
    <div class="flex flex-wrap items-end gap-3">
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('user.username')}</span>
        <Input bind:value={filters.username} onkeydown={(e) => e.key === 'Enter' && search()} />
      </label>
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('common.status')}</span>
        <NativeSelect class="w-full" bind:value={filters.status}>
          <option value="">{t('common.all')}</option>
          <option value="success">{t('loginlog.status.success')}</option>
          <option value="failed">{t('loginlog.status.failed')}</option>
        </NativeSelect>
      </label>

      <!-- 按钮跟在字段后面，一起换行：字段改成固定宽度后，
           再把按钮推到最右边会显得很散，而且中间空一大片 -->
      <div class="flex gap-2">
        <Button variant="outline" onclick={resetFilters}>{t('common.reset')}</Button>
        <Button onclick={search}>{t('common.search')}</Button>
      </div>
    </div>
  </div>

  <p class="text-sm text-muted-foreground">{t('common.total', { total })}</p>

  <div class="rounded-lg border bg-card">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('common.status')}</TableHead>
          <TableHead>{t('user.username')}</TableHead>
          <TableHead>{t('loginlog.reason')}</TableHead>
          <TableHead>{t('session.ip')}</TableHead>
          <TableHead>{t('session.userAgent')}</TableHead>
          <TableHead>{t('session.loginAt')}</TableHead>
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
        {#each rows as log (log.id)}
          <TableRow>
            <TableCell>
              <Badge variant={log.status === 'success' ? 'default' : 'destructive'}>
                {t(log.status === 'success' ? 'loginlog.status.success' : 'loginlog.status.failed')}
              </Badge>
            </TableCell>
            <TableCell class="font-medium">{log.username}</TableCell>
            <TableCell class="text-sm text-muted-foreground">
              <!-- reason 是后端给的 i18n 键，与响应体保持一致 -->
              {log.reason ? tKey(log.reason) : '—'}
            </TableCell>
            <TableCell class="font-mono text-xs">{log.ip || '—'}</TableCell>
            <TableCell class="max-w-64 truncate text-xs text-muted-foreground" title={log.user_agent}>
              {log.user_agent || '—'}
            </TableCell>
            <TableCell class="text-muted-foreground">
              {new Date(log.created_at).toLocaleString()}
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
