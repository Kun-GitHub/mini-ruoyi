<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api } from '$lib/api/client'
  import type { PermCatalogue } from '$lib/api/types'
  import { Badge } from '$lib/components/ui/badge'
  import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '$lib/components/ui/table'
  import { t, tKey } from '$lib/i18n/index.svelte'
  import { notifyError } from '$lib/stores/notify.svelte'

  let groups = $state<PermCatalogue['groups']>([])
  let loading = $state(false)

  onMount(async () => {
    loading = true
    try {
      groups = (await api.get<PermCatalogue>('/perms')).groups
    } catch (err) {
      notifyError(err instanceof ApiError ? err.key : 'error.internal')
    } finally {
      loading = false
    }
  })

  const total = $derived(groups.reduce((n, g) => n + g.perms.length, 0))
</script>

<div class="flex flex-col gap-4">
  <!--
    这一页刻意只读。
    权限点不是「数据」，是「代码 + 路由 + 校验」三者的组合：只往库里写一行
    权限码，它不会被任何路由引用，勾了也不生效。所以能做的只有查看和授权，
    授权在角色那边。
  -->
  <div class="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
    <p>{t('api.readonly')}</p>
    <p class="mt-2">{t('api.grantHint')}</p>
  </div>

  <p class="text-sm text-muted-foreground">{t('common.total', { total })}</p>

  {#if loading}
    <p class="py-10 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
  {:else}
    <div class="flex flex-col gap-6">
      {#each groups as group (group.title_key)}
        <section class="flex flex-col gap-2">
          <h2 class="text-sm font-medium">{tKey(group.title_key)}</h2>
          <div class="overflow-hidden rounded-lg border bg-card">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('api.code')}</TableHead>
                  <TableHead>{t('api.label')}</TableHead>
                  <TableHead>{t('api.endpoints')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each group.perms as item (item.code)}
                  <TableRow>
                    <TableCell class="font-mono text-xs whitespace-nowrap">{item.code}</TableCell>
                    <TableCell class="whitespace-nowrap">{tKey(item.label_key)}</TableCell>
                    <TableCell>
                      {#if item.endpoints.length === 0}
                        <!-- 不该出现：权限码声明了却没有路由用它。有用例
                             TestEveryDeclaredPermIsUsedByARoute 兜住。 -->
                        <span class="text-sm text-destructive">{t('api.noEndpoint')}</span>
                      {:else}
                        <div class="flex flex-wrap gap-1.5">
                          {#each item.endpoints as endpoint (`${endpoint.method} ${endpoint.path}`)}
                            <Badge variant="outline" class="font-mono text-xs font-normal">
                              {endpoint.method} {endpoint.path}
                            </Badge>
                          {/each}
                        </div>
                      {/if}
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          </div>
        </section>
      {/each}
    </div>
  {/if}
</div>
