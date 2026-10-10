<script lang="ts">
  import { Button } from '$lib/components/ui/button'

  let {
    page,
    totalPages,
    onGo,
  }: {
    page: number
    totalPages: number
    onGo: (target: number) => void
  } = $props()
</script>

{#if totalPages > 1}
  <div class="flex items-center justify-end gap-2 text-sm">
    <Button variant="outline" size="sm" disabled={page <= 1} onclick={() => onGo(page - 1)}>‹</Button>
    <span class="text-muted-foreground tabular-nums">{page} / {totalPages}</span>
    <Button variant="outline" size="sm" disabled={page >= totalPages} onclick={() => onGo(page + 1)}>›</Button>
  </div>
{/if}
