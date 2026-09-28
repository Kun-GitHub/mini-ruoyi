<script lang="ts">
  // 带进度条的指标。自己画而不是引入 shadcn 的 Progress：
  // 一个纯展示的条不值多一个组件依赖。
  let { label, value, percent }: { label: string; value: string; percent: number } = $props()

  // 1C1G 的机器上，80% 和 90% 值得一眼看出来
  const barClass = $derived(
    percent >= 90 ? 'bg-destructive' : percent >= 80 ? 'bg-amber-500' : 'bg-primary',
  )
  const width = $derived(Math.min(100, Math.max(0, percent)))
</script>

<div class="flex flex-col gap-1.5">
  <div class="flex items-center justify-between">
    <span class="text-muted-foreground">{label}</span>
    <span class="font-medium tabular-nums">{value}</span>
  </div>
  <div class="h-1.5 w-full overflow-hidden rounded-full bg-muted">
    <div class="h-full rounded-full transition-all {barClass}" style="width: {width}%"></div>
  </div>
</div>
