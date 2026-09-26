<script lang="ts">
  import { onMount } from 'svelte'
  import { Badge } from '$lib/components/ui/badge'
  import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card'

  let backend: 'checking' | 'ok' | 'unreachable' = $state('checking')

  onMount(async () => {
    try {
      // 只判断连通性：响应信封格式尚未定稿，这里不依赖 code 字段
      const res = await fetch('/healthz')
      backend = res.ok ? 'ok' : 'unreachable'
    } catch {
      backend = 'unreachable'
    }
  })
</script>

<main class="mx-auto flex min-h-svh max-w-3xl flex-col justify-center gap-6 p-6">
  <Card>
    <CardHeader>
      <CardTitle>mini-ruoyi</CardTitle>
      <CardDescription>前端脚手架已就绪，等待业务页面</CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-3 text-sm">
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground">后端连通性</span>
        {#if backend === 'checking'}
          <Badge variant="secondary">检测中</Badge>
        {:else if backend === 'ok'}
          <Badge>正常</Badge>
        {:else}
          <Badge variant="destructive">不可达</Badge>
        {/if}
      </div>
      <p class="text-muted-foreground">
        该页面用于验证 Svelte 5 + Vite + Tailwind v4 + shadcn-svelte 工具链，
        以及后端从磁盘托管前端产物的链路是否打通。
      </p>
    </CardContent>
  </Card>
</main>
