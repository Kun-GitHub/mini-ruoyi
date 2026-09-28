<script lang="ts">
  // 一行「标签 —— 值」。运行状态页里反复出现，抽出来避免十几次重复的 flex 布局。
  let { label, value, mono = false }: { label: string; value: string; mono?: boolean } = $props()
</script>

<div class="flex items-center justify-between gap-4">
  <span class="text-muted-foreground">{label}</span>
  <span class="tabular-nums {mono ? 'font-mono text-xs' : ''}">{value}</span>
</div>
