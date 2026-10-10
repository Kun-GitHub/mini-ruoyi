<script lang="ts">
  import { onMount } from 'svelte'

  import type { OperLog } from '$lib/api/types'
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

  const list = createListState<OperLog, 'username' | 'method' | 'path' | 'start' | 'end'>({
    endpoint: '/oper-logs',
    filterKeys: ['username', 'method', 'path', 'start', 'end'],
    exportPath: '/api/v1/oper-logs/export',
  })

  onMount(() => {
    void list.load()
  })
</script>

<div class="flex flex-col gap-4">
  <div class="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
    <p>{t('operlog.hint')}</p>
  </div>

  <FilterBar onSearch={list.search} onReset={list.reset}>
    <label class="flex w-48 flex-col gap-1.5 text-sm">
      <span class="text-muted-foreground">{t('user.username')}</span>
      <Input bind:value={list.filters.username} />
    </label>
    <label class="flex w-48 flex-col gap-1.5 text-sm">
      <span class="text-muted-foreground">{t('operlog.method')}</span>
      <NativeSelect class="w-full" bind:value={list.filters.method}>
        <option value="">{t('common.all')}</option>
        <option value="POST">POST</option>
        <option value="PUT">PUT</option>
        <option value="DELETE">DELETE</option>
      </NativeSelect>
    </label>
    <label class="flex w-48 flex-col gap-1.5 text-sm">
      <span class="text-muted-foreground">{t('operlog.path')}</span>
      <Input bind:value={list.filters.path} />
    </label>
    <label class="flex w-48 flex-col gap-1.5 text-sm">
      <span class="text-muted-foreground">{t('operlog.time')}</span>
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
    <Button data-testid="export-oper-logs" variant="outline" size="sm" href={list.exportHref}>
      {t('common.export')}
    </Button>
  </ListHeader>

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
        {#if list.rows.length === 0}
          <TableEmpty loading={list.loading} colspan={7} />
        {/if}
        {#each list.rows as log (log.id)}
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

  <Pagination page={list.page} totalPages={list.totalPages} onGo={list.goPage} />
</div>
