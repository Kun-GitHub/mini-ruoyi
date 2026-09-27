<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api } from '$lib/api/client'
  import type { Job } from '$lib/api/types'
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
  import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
  } from '$lib/components/ui/dialog'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
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
  import { notifyError, notifySuccess } from '$lib/stores/notify.svelte'
  import { session } from '$lib/stores/session.svelte'

  let rows = $state<Job[]>([])
  let loading = $state(false)

  async function load() {
    loading = true
    try {
      rows = (await api.get<{ list: Job[] }>('/jobs')).list
    } catch (err) {
      notifyError(err instanceof ApiError ? err.key : 'error.internal')
    } finally {
      loading = false
    }
  }

  onMount(() => {
    void load()
  })

  // ---- 编辑 ----

  let editTarget = $state<Job | null>(null)
  let cron = $state('')
  let status = $state<'active' | 'inactive'>('active')
  let remark = $state('')
  let saving = $state(false)

  function openEdit(job: Job) {
    editTarget = job
    cron = job.cron
    status = job.status
    remark = job.remark
  }

  async function save(event: SubmitEvent) {
    event.preventDefault()
    if (!editTarget) return
    saving = true
    try {
      await api.put(`/jobs/${editTarget.job_key}`, { cron, status, remark })
      notifySuccess('notify.saved')
      editTarget = null
      await load()
    } catch (err) {
      notifyError(err instanceof ApiError ? err.key : 'error.internal')
    } finally {
      saving = false
    }
  }

  // ---- 立即执行 ----

  let running = $state<string | null>(null)

  async function runNow(job: Job) {
    running = job.job_key
    try {
      // 后端是异步执行的（任务耗时不可控，同步等待会挂住请求最多 5 分钟），
      // 所以这里只提示「已触发」，结果要再刷新一次
      await api.post(`/jobs/${job.job_key}/run`)
      notifySuccess('notify.triggered')
      // 等一会儿再刷新，让短任务的结果能显示出来
      setTimeout(() => void load(), 1500)
    } catch (err) {
      notifyError(err instanceof ApiError ? err.key : 'error.internal')
    } finally {
      running = null
    }
  }

  function formatTime(v: string | null): string {
    return v ? new Date(v).toLocaleString() : t('job.never')
  }
</script>

<div class="flex flex-col gap-4">
  <div class="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
    <p>{t('job.hint')}</p>
  </div>

  <div class="rounded-lg border bg-card">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('job.key')}</TableHead>
          <TableHead>{t('job.cron')}</TableHead>
          <TableHead>{t('common.status')}</TableHead>
          <TableHead>{t('job.nextRun')}</TableHead>
          <TableHead>{t('job.lastRun')}</TableHead>
          <TableHead>{t('job.lastResult')}</TableHead>
          <TableHead class="text-right">{t('common.actions')}</TableHead>
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
        {#each rows as job (job.job_key)}
          <TableRow>
            <TableCell>
              <div class="font-medium">{tKey(job.description_key)}</div>
              <div class="font-mono text-xs text-muted-foreground">{job.job_key}</div>
            </TableCell>
            <TableCell class="font-mono text-xs">{job.cron}</TableCell>
            <TableCell>
              <Badge variant={job.status === 'active' ? 'default' : 'secondary'}>
                {t(job.status === 'active' ? 'common.status.active' : 'common.status.inactive')}
              </Badge>
            </TableCell>
            <TableCell class="text-muted-foreground">
              <!-- 停用的任务没有下次执行时间 -->
              {job.status === 'active' ? formatTime(job.next_run_at) : '—'}
            </TableCell>
            <TableCell class="text-muted-foreground">{formatTime(job.last_run_at)}</TableCell>
            <TableCell>
              {#if job.last_status === ''}
                <!-- 左边「上次执行」列已经写了「从未执行」，这里再写一遍是冗余 -->
                <span class="text-muted-foreground">—</span>
              {:else}
                <Badge
                  variant={job.last_status === 'success'
                    ? 'default'
                    : job.last_status === 'skipped'
                      ? 'secondary'
                      : 'destructive'}
                >
                  {t(`job.status.${job.last_status}` as 'job.status.success')}
                </Badge>
                {#if job.last_status === 'failed'}
                  <!-- 失败原因直接显示出来，不用点进去找 -->
                  <div class="mt-1 max-w-64 truncate text-xs text-destructive" title={job.last_error}>
                    {job.last_error}
                  </div>
                {/if}
                <div class="text-xs text-muted-foreground">{job.last_duration_ms} ms</div>
              {/if}
            </TableCell>
            <TableCell class="text-right whitespace-nowrap">
              {#if session.can('tool:job:run')}
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={running === job.job_key}
                  onclick={() => runNow(job)}
                >
                  {t('job.runNow')}
                </Button>
              {/if}
              {#if session.can('tool:job:edit')}
                <Button variant="ghost" size="sm" onclick={() => openEdit(job)}>
                  {t('common.edit')}
                </Button>
              {/if}
            </TableCell>
          </TableRow>
        {/each}
      </TableBody>
    </Table>
  </div>
</div>

<Dialog open={editTarget !== null} onOpenChange={(open) => !open && (editTarget = null)}>
  <DialogContent class="max-w-md">
    <form onsubmit={save}>
      <DialogHeader>
        <DialogTitle>{editTarget ? tKey(editTarget.description_key) : ''}</DialogTitle>
        <DialogDescription>{t('job.cronHint')}</DialogDescription>
      </DialogHeader>

      <div class="grid gap-4 py-4">
        <div class="grid gap-1.5">
          <Label for="j-cron">{t('job.cron')}</Label>
          <Input id="j-cron" bind:value={cron} required maxlength={64} placeholder="0 3 * * *" />
        </div>
        <div class="grid gap-1.5">
          <Label for="j-status">{t('common.status')}</Label>
          <NativeSelect id="j-status" bind:value={status}>
            <option value="active">{t('common.status.active')}</option>
            <option value="inactive">{t('common.status.inactive')}</option>
          </NativeSelect>
        </div>
        <div class="grid gap-1.5">
          <Label for="j-remark">{t('field.remark')}</Label>
          <Input id="j-remark" bind:value={remark} maxlength={255} />
        </div>
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onclick={() => (editTarget = null)}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" disabled={saving}>{t('common.save')}</Button>
      </DialogFooter>
    </form>
  </DialogContent>
</Dialog>
