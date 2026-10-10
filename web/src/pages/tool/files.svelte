<script lang="ts">
  import { onMount } from 'svelte'

  import { api } from '$lib/api/client'
  import type { FileItem, FilePage } from '$lib/api/types'
  import FilterBar from '$lib/components/filter-bar.svelte'
  import ListHeader from '$lib/components/list-header.svelte'
  import Pagination from '$lib/components/pagination.svelte'
  import TableEmpty from '$lib/components/table-empty.svelte'
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
  import { createListState, errKey } from '$lib/list-state.svelte'
  import { confirm } from '$lib/stores/confirm.svelte'
  import { notifyError, notifySuccess } from '$lib/stores/notify.svelte'
  import { session } from '$lib/stores/session.svelte'

  let usage = $state({ used: 0, quota: 0, count: 0, max_size: 0 })

  const list = createListState<FileItem, 'name' | 'group', FilePage>({
    endpoint: '/files',
    filterKeys: ['name', 'group'],
    // 用量随列表一起返回，省掉一次往返
    onResponse: (res) => (usage = res.usage),
  })

  onMount(() => {
    void list.load()
  })

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
      list.search()
    } catch (err) {
      notifyError(errKey(err))
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
      await list.refreshAfterDelete()
    } catch (err) {
      notifyError(errKey(err))
    }
  }

  // ---- 展示辅助 ----

  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
    return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
  }
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

  <FilterBar onSearch={list.search} onReset={list.reset}>
    <label class="flex w-48 flex-col gap-1.5 text-sm">
      <span class="text-muted-foreground">{t('file.name')}</span>
      <Input bind:value={list.filters.name} />
    </label>
    <label class="flex w-48 flex-col gap-1.5 text-sm">
      <span class="text-muted-foreground">{t('file.group')}</span>
      <Input bind:value={list.filters.group} />
    </label>
  </FilterBar>

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

  <ListHeader total={list.total} />

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
        {#if list.rows.length === 0}
          <TableEmpty loading={list.loading} colspan={6} />
        {/if}
        {#each list.rows as item (item.id)}
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

  <Pagination page={list.page} totalPages={list.totalPages} onGo={list.goPage} />
</div>
