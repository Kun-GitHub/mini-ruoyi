<script lang="ts">
  import type { Snippet } from 'svelte'

  import { Button } from '$lib/components/ui/button'
  import { t } from '$lib/i18n/index.svelte'

  let {
    onSearch,
    onReset,
    children,
  }: {
    onSearch: () => void
    onReset: () => void
    children: Snippet
  } = $props()

  function submit(event: SubmitEvent) {
    event.preventDefault()
    onSearch()
  }
</script>

<!-- 回车即搜索靠 <form> 的隐式提交：输入框里按一下回车等于点「搜索」，
     不必给每个字段再绑一次 keydown。
     重置按钮保持 Button 组件默认的 type="button"：它若跟着提交，
     「清空」就会变成「按清空后的条件再搜一次」，看着像没生效。 -->
<form data-testid="filters" class="rounded-lg border bg-card p-4" onsubmit={submit}>
  <div class="flex flex-wrap items-end gap-3">
    {@render children()}

    <!-- 按钮跟在字段后面，一起换行：字段改成固定宽度后，
         再把按钮推到最右边会显得很散，而且中间空一大片 -->
    <div class="flex gap-2">
      <Button variant="outline" onclick={onReset}>{t('common.reset')}</Button>
      <Button type="submit">{t('common.search')}</Button>
    </div>
  </div>
</form>
