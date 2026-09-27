<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api } from '$lib/api/client'
  import type { OperLog, Page } from '$lib/api/types'
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

  const initial = new URLSearchParams(route.search)
  let page = $state(Math.max(1, Number(initial.get('page') ?? 1) || 1))
  let filters = $state({
    username: initial.get('username') ?? '',
    method: initial.get('method') ?? '',
    path: initial.get('path') ?? '',
  })

  let rows = $state<OperLog[]>([])
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
      if (filters.method) q.set('method', filters.method)
      if (filters.path) q.set('path', filters.path)

      const res = await api.get<Page<OperLog>>(`/oper-logs?${q}`)
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
    filters = { username: '', method: '', path: '' }
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
    <p>{t('operlog.hint')}</p>
  </div>

  <div data-testid="filters" class="rounded-lg border bg-card p-4">
    <div class="flex flex-wrap items-end gap-3">
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('user.username')}</span>
        <Input bind:value={filters.username} onkeydown={(e) => e.key === 'Enter' && search()} />
      </label>
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('operlog.method')}</span>
        <NativeSelect class="w-full" bind:value={filters.method}>
          <option value="">{t('common.all')}</option>
          <option value="POST">POST</option>
          <option value="PUT">PUT</option>
          <option value="DELETE">DELETE</option>
        </NativeSelect>
      </label>
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('operlog.path')}</span>
        <Input bind:value={filters.path} onkeydown={(e) => e.key === 'Enter' && search()} />
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
          <TableHead>{t('user.username')}</TableHead>
          <TableHead>{t('operlog.method')}</TableHead>
          <TableHead>{t('operlog.path')}</TableHead>
          <TableHead>{t('operlog.result')}</TableHead>
          <TableHead class="text-right">{t('operlog.duration')}</TableHead>
          <TableHead>{t('session.ip')}</TableHead>
          <TableHead>{t('common.time')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#if rows.length === 0}
          <TableRow>
            <TableCell colspan={7} class="py-10 text-center text-muted-foreground">
              {loading ? t('common.loading') : t('common.empty')}
            </TableCell>
          </TableRow>
        {/if}
        {#each rows as log (log.id)}
          <TableRow>
            <TableCell class="font-medium">{log.username || '—'}</TableCell>
            <TableCell>
              <Badge variant={log.method === 'DELETE' ? 'destructive' : 'outline'} class="font-mono">
                {log.method}
              </Badge>
            </TableCell>
            <TableCell class="font-mono text-xs">{log.path}</TableCell>
            <TableCell class="text-sm">
              {#if log.status < 400}
                <span class="text-muted-foreground">{t('operlog.success')}</span>
              {:else}
                <!-- 失败时显示原因键的翻译；越权试探也在这里体现 -->
                <span class="text-destructive">
                  {log.status} {log.result ? tKey(log.result) : ''}
                </span>
              {/if}
            </TableCell>
            <TableCell class="text-right text-xs tabular-nums text-muted-foreground">
              {log.duration_ms} ms
            </TableCell>
            <TableCell class="font-mono text-xs">{log.ip || '—'}</TableCell>
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
