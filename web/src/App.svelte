<script lang="ts">
  import { onMount } from 'svelte'
  import { Badge } from '$lib/components/ui/badge'
  import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card'
  import { getLocale, locales, setLocale, t, type Locale } from '$lib/i18n/index.svelte'

  let backend: 'checking' | 'ok' | 'unreachable' = $state('checking')

  onMount(async () => {
    try {
      // 只判断连通性：这里不依赖响应信封的内容
      const res = await fetch('/healthz')
      backend = res.ok ? 'ok' : 'unreachable'
    } catch {
      backend = 'unreachable'
    }
  })

  const statusKeys = {
    checking: 'home.status.checking',
    ok: 'home.status.ok',
    unreachable: 'home.status.unreachable',
  } as const
</script>

<main class="mx-auto flex min-h-svh max-w-3xl flex-col justify-center gap-6 p-6">
  <Card>
    <CardHeader>
      <div class="flex items-start justify-between gap-4">
        <div>
          <CardTitle>{t('app.name')}</CardTitle>
          <CardDescription>{t('app.description')}</CardDescription>
        </div>
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted-foreground">{t('app.localeLabel')}</span>
          <select
            class="h-8 rounded-md border bg-background px-2 text-sm"
            value={getLocale()}
            onchange={(e) => setLocale((e.currentTarget as HTMLSelectElement).value as Locale)}
          >
            {#each Object.entries(locales) as [code, label] (code)}
              <option value={code}>{label}</option>
            {/each}
          </select>
        </div>
      </div>
    </CardHeader>
    <CardContent class="flex flex-col gap-3 text-sm">
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground">{t('home.backendStatus')}</span>
        {#if backend === 'ok'}
          <Badge>{t(statusKeys[backend])}</Badge>
        {:else if backend === 'checking'}
          <Badge variant="secondary">{t(statusKeys[backend])}</Badge>
        {:else}
          <Badge variant="destructive">{t(statusKeys[backend])}</Badge>
        {/if}
      </div>
      <p class="text-muted-foreground">{t('home.hint')}</p>
    </CardContent>
  </Card>
</main>
