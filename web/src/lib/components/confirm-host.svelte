<script lang="ts">
  import { onMount } from 'svelte'

  import { Button } from '$lib/components/ui/button'
  import { t, tKey } from '$lib/i18n/index.svelte'
  import { confirmState, resolveConfirm } from '$lib/stores/confirm.svelte'

  // Esc 关窗。挂在 window 上而不是对话框 div 上：
  // 后者没有 tabindex 就收不到键盘事件，而给它加 tabindex 又会影响焦点行为。
  onMount(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && confirmState.current) resolveConfirm(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
</script>

{#if confirmState.current}
  {@const req = confirmState.current}
  <!--
    删除有子数据的资源时，后端返回 409 + 影响面，前端据此弹这个框。
    它出现的路径是「点删除 → 后端说需要确认 → 弹框」，
    所以界面上不能同时闪一个错误提示（见 client.ts 的 needsConfirmation）。
  -->
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
    <!-- 遮罩单独一层，点击关闭；对话框另起一层，避免事件冒泡干扰 -->
    <button
      type="button"
      class="absolute inset-0 cursor-default"
      aria-label={t('common.cancel')}
      onclick={() => resolveConfirm(false)}
    ></button>

    <!-- role/aria 不能省：没有它们读屏不会把这块当成对话框，
         焦点也不会被合理地限制在框内 -->
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-title"
      class="relative w-full max-w-md rounded-lg border bg-card p-6 shadow-xl"
    >
      <h2 id="confirm-title" class="text-lg font-medium">{tKey(req.titleKey)}</h2>

      {#if req.bodyKey}
        <p class="mt-2 text-sm text-muted-foreground">{tKey(req.bodyKey)}</p>
      {/if}

      {#if req.details?.length}
        <ul class="mt-4 flex flex-col gap-1 rounded-md border bg-muted/40 p-3 text-sm">
          {#each req.details as detail (detail.labelKey)}
            <li class="flex justify-between gap-4">
              <span class="text-muted-foreground">{tKey(detail.labelKey)}</span>
              <span class="font-medium tabular-nums">{detail.value}</span>
            </li>
          {/each}
        </ul>
      {/if}

      <div class="mt-6 flex justify-end gap-2">
        <Button variant="outline" onclick={() => resolveConfirm(false)}>{t('common.cancel')}</Button>
        <Button variant={req.danger ? 'destructive' : 'default'} onclick={() => resolveConfirm(true)}>
          {t('common.confirm')}
        </Button>
      </div>
    </div>
  </div>
{/if}
