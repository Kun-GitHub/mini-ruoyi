<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api } from '$lib/api/client'
  import type { SessionView } from '$lib/api/types'
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
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

  const initial = new URLSearchParams(route.search)
  let page = $state(Math.max(1, Number(initial.get('page') ?? 1) || 1))
  let rows = $state<SessionView[]>([])
  let total = $state(0)
  let loading = $state(false)

  onMount(() => {
    void load()
  })

  async function load() {
    loading = true
    try {
      const res = await api.get<{
        list: SessionView[]
        total: number
        page: number
        page_size: number
      }>(`/sessions?page=${page}&page_size=${PAGE_SIZE}`)
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
    replaceQuery({ page: page === 1 ? undefined : page })
  }

  function goPage(target: number) {
    page = target
    syncURL()
    void load()
  }

  async function kick(s: SessionView) {
    const ok = await confirm({
      titleKey: 'confirm.deleteTitle',
      bodyKey: 'session.kickConfirm',
      details: [{ labelKey: 'session.user', value: s.username }],
      danger: true,
    })
    if (!ok) return

    try {
      await api.del(`/sessions/${s.token_hash}`)
      notifySuccess('notify.kicked')
      await load()
    } catch (err) {
      notifyError(err instanceof ApiError ? err.key : 'error.internal')
    }
  }

  const totalPages = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)))
</script>

<div class="flex flex-col gap-4">
  <div class="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
    <p>{t('session.hint')}</p>
  </div>

  <p class="text-sm text-muted-foreground">{t('common.total', { total })}</p>

  <div class="rounded-lg border bg-card">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('session.user')}</TableHead>
          <TableHead>{t('session.ip')}</TableHead>
          <TableHead>{t('session.loginAt')}</TableHead>
          <TableHead>{t('session.lastSeen')}</TableHead>
          <TableHead>{t('session.userAgent')}</TableHead>
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
        {#each rows as item (item.token_hash)}
          <TableRow>
            <TableCell>
              <span class="font-medium">{item.username}</span>
              {#if item.user_id === session.user?.id}
                <!-- 标出自己那条：踢自己是会报错的，先让人看出来 -->
                <Badge variant="secondary" class="ml-2">{t('session.current')}</Badge>
              {/if}
            </TableCell>
            <TableCell class="font-mono text-xs">{item.ip || '—'}</TableCell>
            <TableCell class="text-muted-foreground">
              {new Date(item.created_at).toLocaleString()}
            </TableCell>
            <TableCell class="text-muted-foreground">
              {new Date(item.last_seen_at).toLocaleString()}
            </TableCell>
            <TableCell class="max-w-64 truncate text-xs text-muted-foreground" title={item.user_agent}>
              {item.user_agent || '—'}
            </TableCell>
            <TableCell class="text-right whitespace-nowrap">
              {#if session.can('monitor:session:kick')}
                <Button
                  variant="ghost"
                  size="sm"
                  class="text-destructive"
                  disabled={item.user_id === session.user?.id}
                  onclick={() => kick(item)}
                >
                  {t('session.kick')}
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
