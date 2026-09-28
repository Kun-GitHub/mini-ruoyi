<script lang="ts">
  import { ApiError, api } from '$lib/api/client'
  import { deleteWithConfirm } from '$lib/api/delete'
  import type { FieldError, Menu, MenuNode } from '$lib/api/types'
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
  import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
  } from '$lib/components/ui/dialog'
  import { Input } from '$lib/components/ui/input'
  import { NativeSelect } from '$lib/components/ui/native-select'
  import { Label } from '$lib/components/ui/label'
  import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '$lib/components/ui/table'
  import { fieldErrorOf } from '$lib/i18n/errors'
  import { t, tKey } from '$lib/i18n/index.svelte'
  import { notifyError, notifySuccess } from '$lib/stores/notify.svelte'
  import { session } from '$lib/stores/session.svelte'

  let tree = $state<MenuNode[]>([])
  let loading = $state(false)

  async function load() {
    loading = true
    try {
      tree = (await api.get<{ tree: MenuNode[] }>('/menus')).tree
    } catch (err) {
      notifyError(errKey(err))
    } finally {
      loading = false
    }
  }

  $effect(() => {
    void load()
  })

  function errKey(err: unknown): string {
    return err instanceof ApiError ? err.key : 'error.internal'
  }

  /** 压平成「深度 + 节点」的列表，用缩进渲染成树形表格。 */
  type Row = { node: MenuNode; depth: number }
  const rows = $derived(
    (() => {
      const out: Row[] = []
      const walk = (nodes: MenuNode[], depth: number) => {
        for (const node of nodes) {
          out.push({ node, depth })
          walk(node.children, depth + 1)
        }
      }
      walk(tree, 0)
      return out
    })(),
  )

  // ---- 新建 / 编辑 ----

  type Form = {
    id: number | null
    parent_id: number | null
    sort: number
    menu_type: 'directory' | 'menu'
    title_key: string
    path: string
    component: string
    icon: string
    status: 'active' | 'inactive'
  }

  const emptyForm = (): Form => ({
    id: null,
    parent_id: null,
    sort: 0,
    menu_type: 'menu',
    title_key: '',
    path: '',
    component: '',
    icon: '',
    status: 'active',
  })

  let form = $state<Form>(emptyForm())
  let formOpen = $state(false)

  /** 可选的上级：只允许挂在目录下面，且不能选到自己（后者后端也会拦，这里先挡掉避免无谓往返）。 */
  const parentOptions = $derived(
    rows.filter((r) => r.node.menu_type === 'directory' && r.node.id !== form.id).map((r) => r.node),
  )
  let saving = $state(false)
  let fieldErrors = $state<FieldError[]>([])

  function openCreate() {
    form = emptyForm()
    fieldErrors = []
    formOpen = true
  }

  function openEdit(node: Menu) {
    form = {
      id: node.id,
      parent_id: node.parent_id,
      sort: node.sort,
      menu_type: node.menu_type,
      title_key: node.title_key,
      path: node.path,
      component: node.component,
      icon: node.icon,
      status: node.status,
    }
    fieldErrors = []
    formOpen = true
  }

  async function save(event: SubmitEvent) {
    event.preventDefault()
    saving = true
    fieldErrors = []
    const body = {
      parent_id: form.parent_id,
      sort: form.sort,
      menu_type: form.menu_type,
      title_key: form.title_key,
      path: form.path,
      component: form.menu_type === 'directory' ? '' : form.component,
      icon: form.icon,
      status: form.status,
    }
    try {
      if (form.id === null) await api.post('/menus', body)
      else await api.put(`/menus/${form.id}`, body)
      notifySuccess('notify.saved')
      formOpen = false
      await load()
    } catch (err) {
      if (err instanceof ApiError) {
        fieldErrors = err.fieldErrors
        if (err.fieldErrors.length === 0) notifyError(err.key)
      } else {
        notifyError('error.internal')
      }
    } finally {
      saving = false
    }
  }

  async function remove(node: Menu) {
    try {
      const done = await deleteWithConfirm(`/menus/${node.id}`)
      if (!done) return
      notifySuccess('notify.deleted')
      await load()
    } catch (err) {
      notifyError(errKey(err))
    }
  }

  let busyID = $state<number | null>(null)
</script>

<div class="flex flex-col gap-4">
  <div class="flex items-center justify-between">
    <p class="text-sm text-muted-foreground">
      <!-- 菜单是部署期配置：必须对应一个前端页面组件，所以不能在界面上凭空造一个 -->
      {t('menu.componentHint')}
    </p>
    {#if session.can('system:menu:add')}
      <Button size="sm" onclick={openCreate}>{t('common.create')}</Button>
    {/if}
  </div>

  <div class="rounded-lg border bg-card">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('field.name')}</TableHead>
          <TableHead>{t('menu.type')}</TableHead>
          <TableHead>{t('menu.path')}</TableHead>
          <TableHead>{t('menu.component')}</TableHead>
          <TableHead class="text-right">{t('field.sort')}</TableHead>
          <TableHead>{t('common.status')}</TableHead>
          <TableHead class="text-right">{t('common.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#if rows.length === 0}
          <TableRow>
            <TableCell colspan={7} class="py-10 text-center text-muted-foreground">
              {loading ? t('common.loading') : t('common.empty')}
            </TableCell>
          </TableRow>
        {/if}
        {#each rows as row (row.node.id)}
          <TableRow>
            <TableCell>
              <!-- 用缩进表达层级，而不是嵌套表格：后者在窄屏上会挤成一团 -->
              <span style="padding-left: {row.depth * 1.25}rem" class="font-medium">
                {tKey(row.node.title_key)}
              </span>
              <span class="ml-2 text-xs text-muted-foreground">{row.node.title_key}</span>
            </TableCell>
            <TableCell>
              <Badge variant={row.node.menu_type === 'directory' ? 'secondary' : 'outline'}>
                {t(row.node.menu_type === 'directory' ? 'menu.type.directory' : 'menu.type.menu')}
              </Badge>
            </TableCell>
            <TableCell class="font-mono text-xs">{row.node.path || '—'}</TableCell>
            <TableCell class="font-mono text-xs text-muted-foreground">
              {row.node.component || '—'}
            </TableCell>
            <TableCell class="text-right tabular-nums">{row.node.sort}</TableCell>
            <TableCell>
              <Badge variant={row.node.status === 'active' ? 'default' : 'secondary'}>
                {t(row.node.status === 'active' ? 'common.status.active' : 'common.status.inactive')}
              </Badge>
            </TableCell>
            <TableCell class="text-right whitespace-nowrap">
              {#if session.can('system:menu:edit')}
                <Button variant="ghost" size="sm" onclick={() => openEdit(row.node)}>
                  {t('common.edit')}
                </Button>
              {/if}
              {#if session.can('system:menu:delete')}
                <Button
                  variant="ghost"
                  size="sm"
                  class="text-destructive"
                  disabled={busyID === row.node.id}
                  onclick={async () => {
                    busyID = row.node.id
                    await remove(row.node)
                    busyID = null
                  }}
                >
                  {t('common.delete')}
                </Button>
              {/if}
            </TableCell>
          </TableRow>
        {/each}
      </TableBody>
    </Table>
  </div>
</div>

<Dialog bind:open={formOpen}>
  <DialogContent class="max-h-[85svh] max-w-lg overflow-y-auto">
    <form onsubmit={save}>
      <DialogHeader>
        <DialogTitle>{t(form.id === null ? 'menu.createTitle' : 'menu.editTitle')}</DialogTitle>
      </DialogHeader>

      <div class="grid gap-4 py-4">
        <div class="grid grid-cols-2 gap-4">
          <div class="grid gap-1.5">
            <Label for="m-type">{t('menu.type')}</Label>
            <NativeSelect id="m-type" class="w-full" bind:value={form.menu_type}>
              <option value="directory">{t('menu.type.directory')}</option>
              <option value="menu">{t('menu.type.menu')}</option>
            </NativeSelect>
          </div>
          <div class="grid gap-1.5">
            <Label for="m-parent">{t('menu.parent')}</Label>
            <NativeSelect
              id="m-parent"
              class="w-full"
              value={form.parent_id ?? ''}
              onchange={(e) =>
                (form.parent_id = e.currentTarget.value === '' ? null : Number(e.currentTarget.value))}
            >
              <option value="">{t('menu.parentRoot')}</option>
              {#each parentOptions as option (option.id)}
                <option value={option.id}>{tKey(option.title_key)}</option>
              {/each}
            </NativeSelect>
          </div>
        </div>

        <div class="grid gap-1.5">
          <Label for="m-title">{t('menu.titleKey')}</Label>
          <Input id="m-title" bind:value={form.title_key} required maxlength={128} placeholder="menu.system.users" />
          <p class="text-xs text-muted-foreground">{t('menu.titleKeyHint')}</p>
          {#if fieldErrorOf(fieldErrors, 'title_key')}<p class="text-sm text-destructive">{fieldErrorOf(fieldErrors, 'title_key')}</p>{/if}
        </div>

        <div class="grid grid-cols-2 gap-4">
          <div class="grid gap-1.5">
            <Label for="m-path">{t('menu.path')}</Label>
            <Input id="m-path" bind:value={form.path} maxlength={255} placeholder="/system/users" />
          </div>
          <div class="grid gap-1.5">
            <Label for="m-sort">{t('menu.sort')}</Label>
            <Input id="m-sort" type="number" bind:value={form.sort} min={0} />
          </div>
        </div>

        {#if form.menu_type === 'menu'}
          <div class="grid gap-1.5">
            <Label for="m-component">{t('menu.component')}</Label>
            <Input id="m-component" bind:value={form.component} maxlength={255} placeholder="system/users" />
            <p class="text-xs text-muted-foreground">{t('menu.componentHint')}</p>
            {#if fieldErrorOf(fieldErrors, 'component')}<p class="text-sm text-destructive">{fieldErrorOf(fieldErrors, 'component')}</p>{/if}
          </div>
        {/if}

        <div class="grid grid-cols-2 gap-4">
          <div class="grid gap-1.5">
            <Label for="m-icon">{t('menu.icon')}</Label>
            <Input id="m-icon" bind:value={form.icon} maxlength={64} placeholder="users" />
          </div>
          <div class="grid gap-1.5">
            <Label for="m-status">{t('common.status')}</Label>
            <NativeSelect id="m-status" class="w-full" bind:value={form.status}>
              <option value="active">{t('common.status.active')}</option>
              <option value="inactive">{t('common.status.inactive')}</option>
            </NativeSelect>
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onclick={() => (formOpen = false)}>{t('common.cancel')}</Button>
        <Button type="submit" disabled={saving}>{t('common.save')}</Button>
      </DialogFooter>
    </form>
  </DialogContent>
</Dialog>
