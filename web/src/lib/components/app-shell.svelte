<script lang="ts">
  import type { Component } from 'svelte'

  import { Button } from '$lib/components/ui/button'
  import { NativeSelect } from '$lib/components/ui/native-select'
  import { getLocale, locales, setLocale, t, tKey, type Locale } from '$lib/i18n/index.svelte'
  import { flattenMenus, navigate, replaceTo, resolveComponent, route } from '$lib/router.svelte'
  import { logout, session } from '$lib/stores/session.svelte'
  import {
    closeAll,
    closeOthers,
    closeTab,
    openTab,
    tabState,
    type Tab,
  } from '$lib/stores/tabs.svelte'

  // 目录默认展开。菜单树只有一层目录，所以不用做「记住展开状态」那一套。
  let collapsed = $state<Record<number, boolean>>({})

  const flatMenus = $derived(flattenMenus(session.menus))
  const active = $derived(flatMenus.find((m) => m.path === route.pathname) ?? null)

  /**
   * 导航到菜单页面时开一个标签。
   *
   * 关键点是「路径变了才开」，而不是「当前路径是个菜单页就开」。
   * 后者会让「关闭全部」「关掉当前标签」失效——关掉之后路径没变，
   * effect 重跑又把标签加回来，标签根本关不掉。
   *
   * lastOpened/redirected 是普通变量而非 $state：在 effect 里改它们
   * 不该触发重跑。
   */
  let lastOpened = ''
  let redirected = false

  $effect(() => {
    const path = route.pathname
    const menu = flatMenus.find((m) => m.path === path)

    if (menu) {
      if (path !== lastOpened) {
        lastOpened = path
        openTab({ path: menu.path, titleKey: menu.titleKey, component: menu.component })
      }
      return
    }

    // 首次进入时若路径不在菜单里（登录后落在根路径、或菜单被删了而地址栏留着旧路径），
    // 跳到第一个可访问的页面。只做一次，否则用户关掉全部标签后会被弹回来。
    if (!redirected && flatMenus.length > 0) {
      redirected = true
      replaceTo(flatMenus[0].path)
    }
  })

  /** 每个标签对应一个已解析的组件；解析不到的显示占位。 */
  const rendered = $derived(
    tabState.items.map((tab) => ({ tab, Page: resolveComponent(tab.component) as Component | null })),
  )

  function toggle(id: number) {
    collapsed = { ...collapsed, [id]: !collapsed[id] }
  }

  function onCloseTab(path: string) {
    const wasActive = path === route.pathname
    const nextPath = closeTab(path)
    // 关掉非激活标签时不跳转，用户还在看原来的页面
    if (wasActive && nextPath) navigate(nextPath)
  }

  async function signOut() {
    await logout()
  }
</script>

<div class="flex min-h-svh">
  <!-- 侧边栏 -->
  <aside class="flex w-60 shrink-0 flex-col border-r bg-card">
    <div class="flex h-14 items-center border-b px-4 font-semibold">{t('app.name')}</div>

    <nav data-testid="sidebar" class="flex-1 overflow-y-auto p-2">
      {#each session.menus as node (node.id)}
        {#if node.children.length > 0}
          <button
            type="button"
            class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm font-medium hover:bg-accent"
            onclick={() => toggle(node.id)}
          >
            <span>{tKey(node.title_key)}</span>
            <!-- 展开指示符是纯装饰：不隐藏的话按钮的可访问名字会变成
                 「系统管理 −」，读屏会把符号念出来，选择器也难匹配 -->
            <span class="text-xs text-muted-foreground" aria-hidden="true">
              {collapsed[node.id] ? '+' : '−'}
            </span>
          </button>
          {#if !collapsed[node.id]}
            <ul class="mt-0.5 flex flex-col">
              {#each node.children as child (child.id)}
                <!-- 只渲染有页面组件的项：目录型子节点点了没有去处 -->
                {#if child.component}
                  <li>
                    <button
                      type="button"
                      class="w-full rounded-md py-1.5 pr-3 pl-6 text-left text-sm transition
                        {route.pathname === child.path
                        ? 'bg-accent font-medium text-accent-foreground'
                        : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'}"
                      onclick={() => navigate(child.path)}
                    >
                      {tKey(child.title_key)}
                    </button>
                  </li>
                {/if}
              {/each}
            </ul>
          {/if}
        {:else if node.component}
          <button
            type="button"
            class="w-full rounded-md px-3 py-2 text-left text-sm transition
              {route.pathname === node.path ? 'bg-accent font-medium' : 'hover:bg-accent/60'}"
            onclick={() => navigate(node.path)}
          >
            {tKey(node.title_key)}
          </button>
        {/if}
      {/each}
    </nav>
  </aside>

  <div class="flex min-w-0 flex-1 flex-col">
    <!-- 顶栏 -->
    <header class="flex h-14 shrink-0 items-center justify-between border-b bg-card px-4">
      <h1 class="truncate font-medium">{active ? tKey(active.titleKey) : t('app.name')}</h1>

      <div class="flex items-center gap-3">
        <NativeSelect
          class="w-32"
          value={getLocale()}
          onchange={(e) => setLocale(e.currentTarget.value as Locale)}
          aria-label={t('app.localeLabel')}
        >
          {#each Object.entries(locales) as [code, label] (code)}
            <option value={code}>{label}</option>
          {/each}
        </NativeSelect>

        <span class="text-sm text-muted-foreground">{session.user?.nickname}</span>

        <Button variant="outline" size="sm" onclick={signOut}>{t('nav.logout')}</Button>
      </div>
    </header>

    <!-- 标签栏 -->
    {#if tabState.items.length > 0}
      <!-- data-testid 供 E2E 使用：标签按钮和侧边栏按钮的文案完全相同，
           不加以区分的话选择器会同时命中两者 -->
      <div
        data-testid="tab-bar"
        class="flex h-10 shrink-0 items-center gap-1 border-b bg-muted/30 px-2"
      >
        <div class="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {#each tabState.items as tab (tab.path)}
            <div
              data-testid="tab"
              class="group flex shrink-0 items-center rounded-md border text-sm transition
                {tab.path === route.pathname
                ? 'border-border bg-card font-medium'
                : 'border-transparent text-muted-foreground hover:bg-accent/60'}"
            >
              <button
                type="button"
                class="py-1 pr-1 pl-2.5"
                onclick={() => navigate(tab.path)}
              >
                {tKey(tab.titleKey)}
              </button>
              <button
                type="button"
                data-testid="tab-close"
                class="px-1.5 py-1 text-muted-foreground opacity-0 transition group-hover:opacity-100 focus:opacity-100"
                aria-label="{t('nav.closeTab')}: {tKey(tab.titleKey)}"
                onclick={() => onCloseTab(tab.path)}
              >
                ×
              </button>
            </div>
          {/each}
        </div>

        <div class="flex shrink-0 items-center gap-1 pl-2">
          {#if tabState.items.length > 1}
            <Button variant="ghost" size="sm" onclick={() => closeOthers(route.pathname)}>
              {t('nav.closeOthers')}
            </Button>
          {/if}
          <Button variant="ghost" size="sm" onclick={closeAll}>{t('nav.closeAll')}</Button>
        </div>
      </div>
    {/if}

    <!-- 内容区 -->
    <main class="min-w-0 flex-1 overflow-auto bg-muted/20 p-6">
      {#if tabState.items.length === 0}
        <p class="py-16 text-center text-sm text-muted-foreground">{t('nav.noTabs')}</p>
      {:else}
        <!--
          所有标签的页面同时渲染，用 CSS 隐藏非激活的。
          这正是多标签页的意义：切回来时表单内容、滚动位置、已加载的数据都还在。
          代价是打开的页面越多，常驻的组件实例越多——管理后台的标签数量在个位数量级，
          这个代价可以接受。

          每个页面用 keyed each 保持组件实例；路径相同不会重复创建。
        -->
        {#each rendered as item (item.tab.path)}
          <!-- data-testid 供 E2E 用：所有打开的标签都在 DOM 里（只是隐藏），
               页面内的 testid 会重复出现，选择器必须能限定到当前可见的那一个 -->
          <div data-testid="tab-panel" class:hidden={item.tab.path !== route.pathname}>
            {#if item.Page}
              <item.Page />
            {:else}
              <!-- 菜单指向了不存在的页面组件。菜单是数据库里的数据，
                   可能手工加过、也可能对应页面被删了，这时要给一个能看懂的提示 -->
              <div class="rounded-lg border border-dashed bg-card p-8 text-center">
                <h2 class="font-medium">{t('page.notFound.title')}</h2>
                <p class="mt-2 text-sm text-muted-foreground">
                  {t('page.notFound.body', { component: item.tab.component })}
                </p>
                <p class="mt-1 text-sm text-muted-foreground">{t('page.notFound.hint')}</p>
              </div>
            {/if}
          </div>
        {/each}
      {/if}
    </main>
  </div>
</div>
