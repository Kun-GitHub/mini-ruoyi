<script lang="ts">
  import { TableCell, TableRow } from '$lib/components/ui/table'
  import { t } from '$lib/i18n/index.svelte'

  let { loading, colspan }: { loading: boolean; colspan: number } = $props()
</script>

<!-- 表格没行时的那一行。loading 与 empty 用同一格文案切换，
     而不是各渲染一个整表状态：加载完从「加载中」变「暂无数据」时
     表头不动，视觉上不会闪一下。 -->
<TableRow>
  <TableCell {colspan} class="py-10 text-center text-muted-foreground">
    {loading ? t('common.loading') : t('common.empty')}
  </TableCell>
</TableRow>
