<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api } from '$lib/api/client'
  import type { SystemSnapshot } from '$lib/api/types'
  import MetricBar from '$lib/components/metric-bar.svelte'
  import MetricRow from '$lib/components/metric-row.svelte'
  import { Button } from '$lib/components/ui/button'
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card'
  import { t } from '$lib/i18n/index.svelte'
  import { notifyError } from '$lib/stores/notify.svelte'

  let snap = $state<SystemSnapshot | null>(null)
  let loading = $state(false)
  let loadedAt = $state<Date | null>(null)

  async function load() {
    loading = true
    try {
      snap = await api.get<SystemSnapshot>('/system')
      loadedAt = new Date()
    } catch (err) {
      notifyError(err instanceof ApiError ? err.key : 'error.internal')
    } finally {
      loading = false
    }
  }

  onMount(() => {
    void load()
  })

  function formatBytes(bytes: number): string {
    if (bytes <= 0) return '0 B'
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`
    return `${(bytes / 1024 ** 3).toFixed(2)} GB`
  }

  function formatUptime(seconds: number): string {
    const d = Math.floor(seconds / 86400)
    const h = Math.floor((seconds % 86400) / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (d > 0) return `${d}d ${h}h ${m}m`
    if (h > 0) return `${h}h ${m}m`
    return `${m}m`
  }

</script>

<div class="flex flex-col gap-4">
  <div class="flex flex-wrap items-center justify-between gap-3">
    <p class="text-sm text-muted-foreground">{t('system.hint')}</p>
    <div class="flex items-center gap-2">
      {#if loadedAt}
        <span class="text-xs text-muted-foreground">
          {loadedAt.toLocaleTimeString()}
        </span>
      {/if}
      <Button variant="outline" onclick={load} disabled={loading}>{t('common.search')}</Button>
    </div>
  </div>

  {#if !snap}
    <p class="py-10 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
  {:else}
    <div class="grid gap-4 lg:grid-cols-2">
      <!-- CPU -->
      <Card>
        <CardHeader>
          <CardTitle>{t('system.cpu')}</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-3 text-sm">
          {#if snap.cpu.available}
            <MetricBar label={t('system.usage')} value="{snap.cpu.usage_percent.toFixed(1)}%" percent={snap.cpu.usage_percent} />
          {:else}
            <p class="text-muted-foreground">{t('system.unsupported')}</p>
          {/if}
          <MetricRow label={t('system.cores')} value={String(snap.cpu.cores)} />
          {#if snap.cpu.load_available && snap.cpu.load_avg}
            <MetricRow
              label={t('system.load')}
              value={snap.cpu.load_avg.map((v) => v.toFixed(2)).join(' / ')}
            />
          {/if}
        </CardContent>
      </Card>

      <!-- 内存 -->
      <Card>
        <CardHeader>
          <CardTitle>{t('system.memory')}</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-3 text-sm">
          {#if snap.memory.available}
            <MetricBar
              label={t('system.usage')}
              value="{snap.memory.usage_percent.toFixed(1)}%"
              percent={snap.memory.usage_percent}
            />
            <MetricRow label={t('system.total')} value={formatBytes(snap.memory.total)} />
            <MetricRow label={t('system.used')} value={formatBytes(snap.memory.used)} />
            <!-- 用「可用」而不是「空闲」：/proc/meminfo 的 MemAvailable 含可回收页缓存 -->
            <MetricRow label={t('system.free')} value={formatBytes(snap.memory.free)} />
          {:else}
            <p class="text-muted-foreground">{t('system.unsupported')}</p>
          {/if}
        </CardContent>
      </Card>

      <!-- 磁盘 -->
      <Card>
        <CardHeader>
          <CardTitle>{t('system.disk')}</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-3 text-sm">
          {#if snap.disk.available}
            <MetricBar
              label={t('system.usage')}
              value="{snap.disk.usage_percent.toFixed(1)}%"
              percent={snap.disk.usage_percent}
            />
            <MetricRow label={t('system.total')} value={formatBytes(snap.disk.total)} />
            <MetricRow label={t('system.used')} value={formatBytes(snap.disk.used)} />
            <MetricRow label={t('system.free')} value={formatBytes(snap.disk.free)} />
            <MetricRow label={t('system.diskPath')} value={snap.disk.path} mono />
          {:else}
            <p class="text-muted-foreground">{t('system.unsupported')}</p>
          {/if}
        </CardContent>
      </Card>

      <!-- 进程 -->
      <Card>
        <CardHeader>
          <CardTitle>{t('system.process')}</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-3 text-sm">
          {#if snap.process.rss_available}
            <MetricRow label={t('system.rss')} value={formatBytes(snap.process.rss)} />
          {/if}
          <MetricRow label={t('system.heap')} value={formatBytes(snap.process.heap_alloc)} />
          <MetricRow label={t('system.sys')} value={formatBytes(snap.process.sys)} />
          <MetricRow label={t('system.goroutines')} value={String(snap.process.goroutines)} />
          <MetricRow label={t('system.gc')} value={String(snap.process.num_gc)} />
          <MetricRow label={t('system.uptime')} value={formatUptime(snap.uptime_seconds)} />
          <MetricRow label="Go" value="{snap.go_version} {snap.os}/{snap.arch}" mono />
        </CardContent>
      </Card>
    </div>
  {/if}
</div>
