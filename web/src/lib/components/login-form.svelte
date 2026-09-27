<script lang="ts">
  import { ApiError } from '$lib/api/client'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { NativeSelect } from '$lib/components/ui/native-select'
  import { getLocale, locales, setLocale, t, tKey, type Locale } from '$lib/i18n/index.svelte'
  import { login } from '$lib/stores/session.svelte'

  let username = $state('')
  let password = $state('')
  let submitting = $state(false)
  let errorKey = $state<string | null>(null)

  async function submit(event: SubmitEvent) {
    event.preventDefault()
    submitting = true
    errorKey = null
    try {
      await login(username, password)
    } catch (err) {
      // 只存后端的 i18n 键，组件里不出现任何具体文案
      errorKey = err instanceof ApiError ? err.key : 'error.internal'
    } finally {
      submitting = false
    }
  }
</script>

<main class="flex min-h-svh items-center justify-center bg-muted/30 p-6">
  <div class="w-full max-w-sm">
    <div class="mb-6 flex items-start justify-between gap-4">
      <div>
        <h1 class="text-xl font-semibold">{t('app.name')}</h1>
        <p class="text-sm text-muted-foreground">{t('app.description')}</p>
      </div>
      <NativeSelect
        class="w-32 shrink-0"
        value={getLocale()}
        onchange={(e) => setLocale(e.currentTarget.value as Locale)}
        aria-label={t('app.localeLabel')}
      >
        {#each Object.entries(locales) as [code, label] (code)}
          <option value={code}>{label}</option>
        {/each}
      </NativeSelect>
    </div>

    <form class="flex flex-col gap-4 rounded-lg border bg-card p-6 shadow-sm" onsubmit={submit}>
      <h2 class="text-lg font-medium">{t('login.title')}</h2>

      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('login.username')}</span>
        <!-- svelte-ignore a11y_autofocus -->
        <Input bind:value={username} autocomplete="username" autofocus required />
      </label>

      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('login.password')}</span>
        <Input type="password" bind:value={password} autocomplete="current-password" required />
      </label>

      <!-- 用 aria-live 而不是弹窗：登录失败是预期内的结果，不该打断用户，
           但读屏软件要能念出来 -->
      <p class="min-h-5 text-sm text-destructive" aria-live="polite">
        {#if errorKey}{tKey(errorKey)}{/if}
      </p>

      <Button type="submit" disabled={submitting}>
        {submitting ? t('login.submitting') : t('login.submit')}
      </Button>
    </form>

    <p class="mt-4 text-center text-xs text-muted-foreground">{t('login.hint')}</p>
  </div>
</main>
