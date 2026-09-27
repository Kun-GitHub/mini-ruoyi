<script lang="ts">
  import { onMount } from 'svelte'

  import AppShell from '$lib/components/app-shell.svelte'
  import ConfirmHost from '$lib/components/confirm-host.svelte'
  import LoginForm from '$lib/components/login-form.svelte'
  import Toaster from '$lib/components/toaster.svelte'
  import { t } from '$lib/i18n/index.svelte'
  import { replaceTo } from '$lib/router.svelte'
  import { bootstrap, session } from '$lib/stores/session.svelte'

  // 刷新页面后 cookie 还在，但 CSRF 令牌只在内存里，所以必须重新问一次 /auth/me
  onMount(() => {
    void bootstrap()
  })

  // 未登录时把地址栏也归到 /login，避免分享出去的深链接看起来像是能直接打开
  $effect(() => {
    if (session.phase === 'anonymous') replaceTo('/login')
  })
</script>

{#if session.phase === 'booting'}
  <div class="flex min-h-svh items-center justify-center text-sm text-muted-foreground">
    {t('common.loading')}
  </div>
{:else if session.phase === 'anonymous'}
  <LoginForm />
{:else}
  <AppShell />
{/if}

<!-- 通知与确认框挂在最外层：它们不受登录状态影响，
     登录失败的通知也要能显示 -->
<Toaster />
<ConfirmHost />
