<!-- 有问题欢迎联系 hot_kun@hotmail.com -->
<script lang="ts">
  import { onMount } from 'svelte'

  import type { LoginLog } from '$lib/api/types'
  import FilterBar from '$lib/components/filter-bar.svelte'
  import ListHeader from '$lib/components/list-header.svelte'
  import Pagination from '$lib/components/pagination.svelte'
  import TableEmpty from '$lib/components/table-empty.svelte'
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
  import { createListState } from '$lib/list-state.svelte'

  const list = createListState<LoginLog, 'username' | 'status' | 'start' | 'end'>({
    endpoint: '/login-logs',
    filterKeys: ['username', 'status', 'start', 'end'],
    exportPath: '/api/v1/login-logs/export',
  })

  onMount(() => {
    void list.load()
  })
</script>

<div class="flex flex-col gap-4">
  <div class="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
    <p>{t('loginlog.hint')}</p>
  </div>

  <FilterBar onSearch={list.search} onReset={list.reset}>
    <label class="flex w-48 flex-col gap-1.5 text-sm">
      <span class="text-muted-foreground">{t('user.username')}</span>
      <Input bind:value={list.filters.username} />
    </label>
    <label class="flex w-48 flex-col gap-1.5 text-sm">
      <span class="text-muted-foreground">{t('common.status')}</span>
      <NativeSelect class="w-full" bind:value={list.filters.status}>
        <option value="">{t('common.all')}</option>
        <option value="success">{t('loginlog.status.success')}</option>
        <option value="failed">{t('loginlog.status.failed')}</option>
      </NativeSelect>
    </label>
    <label class="flex w-48 flex-col gap-1.5 text-sm">
      <span class="text-muted-foreground">{t('loginlog.time')}</span>
      <!-- 两个原生 date 输入框并排：范围选择器要么引库，要么自己写弹层，
           而这里只需要两端各一个日期 -->
      <div class="flex items-center gap-2">
        <Input type="date" bind:value={list.filters.start} />
        <span class="text-muted-foreground">—</span>
        <Input type="date" bind:value={list.filters.end} />
      </div>
    </label>
  </FilterBar>

  <ListHeader total={list.total}>
    <Button data-testid="export-login-logs" variant="outline" size="sm" href={list.exportHref}>
      {t('common.export')}
    </Button>
  </ListHeader>

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
        {#if list.rows.length === 0}
          <TableEmpty loading={list.loading} colspan={6} />
        {/if}
        {#each list.rows as log (log.id)}
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

  <Pagination page={list.page} totalPages={list.totalPages} onGo={list.goPage} />
</div>
