<script lang="ts">
  import { onMount } from 'svelte'

  import { api } from '$lib/api/client'
  import type { SessionView } from '$lib/api/types'
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
  import ListHeader from '$lib/components/list-header.svelte'
  import Pagination from '$lib/components/pagination.svelte'
  import TableEmpty from '$lib/components/table-empty.svelte'
  import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '$lib/components/ui/table'
  import { t } from '$lib/i18n/index.svelte'
  import { createListState, errKey } from '$lib/list-state.svelte'
  import { confirm } from '$lib/stores/confirm.svelte'
  import { notifyError, notifySuccess } from '$lib/stores/notify.svelte'
  import { session } from '$lib/stores/session.svelte'

  // 会话列表没有筛选条件：在线会话本来就只有一屏，翻页足够
  const list = createListState<SessionView, never>({
    endpoint: '/sessions',
    filterKeys: [],
  })

  onMount(() => {
    void list.load()
  })

  async function kick(s: SessionView) {
    const ok = await confirm({
      // 不是删除，用通用的「确认删除」会让人以为删了什么东西
      titleKey: 'confirm.kickTitle',
      bodyKey: 'session.kickConfirm',
      details: [{ labelKey: 'session.user', value: s.username }],
      danger: true,
    })
    if (!ok) return

    try {
      await api.del(`/sessions/${s.token_hash}`)
      notifySuccess('notify.kicked')
      await list.load()
    } catch (err) {
      notifyError(errKey(err))
    }
  }
</script>

<div class="flex flex-col gap-4">
  <div class="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
    <p>{t('session.hint')}</p>
  </div>

  <ListHeader total={list.total} />

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
        {#if list.rows.length === 0}
          <TableEmpty loading={list.loading} colspan={6} />
        {/if}
        {#each list.rows as item (item.token_hash)}
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

  <Pagination page={list.page} totalPages={list.totalPages} onGo={list.goPage} />
</div>
