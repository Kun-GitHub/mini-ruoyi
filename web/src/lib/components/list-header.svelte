<script lang="ts">
  import type { Snippet } from 'svelte'

  import { t } from '$lib/i18n/index.svelte'

  /** children 是右侧的操作按钮（导出/新增/导入），没有就不渲染那一块。 */
  let { total, children }: { total: number; children?: Snippet } = $props()
</script>

<!-- 表格上方那一行：左边共 N 条，右边一排操作按钮。
     权限判断不放进这个组件——门控是页面的事，组件只负责排版。 -->
<div class="flex items-center justify-between">
  <p class="text-sm text-muted-foreground">{t('common.total', { total })}</p>
  {#if children}
    <div class="flex items-center gap-2">{@render children()}</div>
  {/if}
</div>
