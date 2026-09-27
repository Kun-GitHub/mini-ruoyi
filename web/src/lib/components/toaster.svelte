<script lang="ts">
  import { tKey } from '$lib/i18n/index.svelte'
  import { dismiss, notices } from '$lib/stores/notify.svelte'
</script>

<!-- 通知层。固定右上角，不参与布局流，不遮挡主内容。 -->
<!-- 放右下角：右上角是主操作按钮（新增/查询）的位置，通知会挡住它们 -->
<div
  data-testid="toaster"
  class="pointer-events-none fixed right-4 bottom-4 z-50 flex w-80 flex-col gap-2"
>
  {#each notices.items as notice (notice.id)}
    <button
      type="button"
      class="pointer-events-auto w-full rounded-md border px-4 py-3 text-left text-sm shadow-lg transition
        {notice.kind === 'error'
        ? 'border-destructive/30 bg-destructive/10 text-destructive'
        : 'border-border bg-card text-foreground'}"
      onclick={() => dismiss(notice.id)}
    >
      {tKey(notice.key, notice.params)}
    </button>
  {/each}
</div>
