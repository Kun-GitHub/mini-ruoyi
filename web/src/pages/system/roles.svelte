<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api } from '$lib/api/client'
  import { deleteWithConfirm } from '$lib/api/delete'
  import type { FieldError, Grants, MenuNode, Page, PermCatalogue, Role } from '$lib/api/types'
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
  import { replaceQuery, route } from '$lib/router.svelte'
  import { notifyError, notifySuccess } from '$lib/stores/notify.svelte'
  import { session } from '$lib/stores/session.svelte'

  const PAGE_SIZE = 20

  /** 分页与筛选是组件本地状态，只在挂载时从 URL 读一次。理由见 users.svelte。 */
  const initial = new URLSearchParams(route.search)
  let page = $state(Math.max(1, Number(initial.get('page') ?? 1) || 1))
  let filters = $state({
    code: initial.get('code') ?? '',
    name: initial.get('name') ?? '',
    status: initial.get('status') ?? '',
  })

  let total = $state(0)
  let rows = $state<Role[]>([])
  let loading = $state(false)

  onMount(() => {
    void load()
  })

  async function load() {
    loading = true
    try {
      const q = new URLSearchParams()
      q.set('page', String(page))
      q.set('page_size', String(PAGE_SIZE))
      for (const key of ['code', 'name', 'status'] as const) {
        if (filters[key]) q.set(key, filters[key])
      }
      const res = await api.get<Page<Role>>(`/roles?${q}`)
      rows = res.list
      total = res.total
      if (res.page !== page) {
        page = res.page
        syncURL()
      }
    } catch (err) {
      notifyError(errKey(err))
    } finally {
      loading = false
    }
  }

  function syncURL() {
    replaceQuery({ ...filters, page: page === 1 ? undefined : page })
  }

  function search() {
    page = 1
    syncURL()
    void load()
  }

  function resetFilters() {
    filters = { code: '', name: '', status: '' }
    page = 1
    syncURL()
    void load()
  }

  function goPage(target: number) {
    page = target
    syncURL()
    void load()
  }

  function errKey(err: unknown): string {
    return err instanceof ApiError ? err.key : 'error.internal'
  }

  // ---- 新建 / 编辑 ----

  type Form = { id: number | null; code: string; name: string; remark: string; status: 'active' | 'inactive' }

  const emptyForm = (): Form => ({ id: null, code: '', name: '', remark: '', status: 'active' })

  let editForm = $state<Form>(emptyForm())
  let formOpen = $state(false)
  let saving = $state(false)
  let fieldErrors = $state<FieldError[]>([])

  async function save(event: SubmitEvent) {
    event.preventDefault()
    saving = true
    fieldErrors = []
    try {
      if (editForm.id === null) {
        await api.post('/roles', {
          code: editForm.code,
          name: editForm.name,
          remark: editForm.remark,
          status: editForm.status,
        })
      } else {
        // code 不可变：它参与鉴权判断
        await api.put(`/roles/${editForm.id}`, {
          name: editForm.name,
          remark: editForm.remark,
          status: editForm.status,
        })
      }
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

  // ---- 授权 ----

  let grantTarget = $state<Role | null>(null)
  let grantMenus = $state<number[]>([])
  let grantPerms = $state<string[]>([])
  let menuTree = $state<MenuNode[]>([])
  let permGroups = $state<PermCatalogue['groups']>([])
  let grantSaving = $state(false)
  let grantLoading = $state(false)

  async function openGrants(role: Role) {
    grantTarget = role
    grantLoading = true
    try {
      const [grants, menus, catalogue] = await Promise.all([
        api.get<Grants>(`/roles/${role.id}/grants`),
        api.get<{ tree: MenuNode[] }>('/menus'),
        api.get<PermCatalogue>('/perms'),
      ])
      grantMenus = grants.menu_ids
      grantPerms = grants.perm_codes
      menuTree = menus.tree
      permGroups = catalogue.groups
    } catch (err) {
      notifyError(errKey(err))
      grantTarget = null
    } finally {
      grantLoading = false
    }
  }

  async function saveGrants() {
    if (!grantTarget) return
    grantSaving = true
    try {
      await api.put(`/roles/${grantTarget.id}/grants`, {
        menu_ids: grantMenus,
        perm_codes: grantPerms,
      })
      notifySuccess('notify.saved')
      grantTarget = null
    } catch (err) {
      notifyError(errKey(err))
    } finally {
      grantSaving = false
    }
  }

  async function remove(role: Role) {
    try {
      const done = await deleteWithConfirm(`/roles/${role.id}`)
      if (!done) return
      notifySuccess('notify.deleted')
      if (rows.length === 1 && page > 1) goPage(page - 1)
      else await load()
    } catch (err) {
      notifyError(errKey(err))
    }
  }

  let busyID = $state<number | null>(null)
  const totalPages = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)))
</script>

<div class="flex flex-col gap-4">
  <div data-testid="filters" class="rounded-lg border bg-card p-4">
    <div class="flex flex-wrap items-end gap-3">
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('role.code')}</span>
        <Input bind:value={filters.code} onkeydown={(e) => e.key === 'Enter' && search()} />
      </label>
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('field.name')}</span>
        <Input bind:value={filters.name} onkeydown={(e) => e.key === 'Enter' && search()} />
      </label>
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('common.status')}</span>
        <NativeSelect class="w-full" bind:value={filters.status}>
          <option value="">{t('common.all')}</option>
          <option value="active">{t('common.status.active')}</option>
          <option value="inactive">{t('common.status.inactive')}</option>
        </NativeSelect>
      </label>

      <!-- 按钮跟在字段后面，一起换行：字段改成固定宽度后，
           再把按钮推到最右边会显得很散，而且中间空一大片 -->
      <div class="flex gap-2">
        <Button variant="outline" onclick={resetFilters}>{t('common.reset')}</Button>
        <Button onclick={search}>{t('common.search')}</Button>
      </div>
    </div>
  </div>

  <div class="flex items-center justify-between">
    <p class="text-sm text-muted-foreground">{t('common.total', { total })}</p>
    {#if session.can('system:role:add')}
      <Button size="sm" onclick={() => { editForm = emptyForm(); fieldErrors = []; formOpen = true }}>
        {t('common.create')}
      </Button>
    {/if}
  </div>

  <div class="rounded-lg border bg-card">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('role.code')}</TableHead>
          <TableHead>{t('field.name')}</TableHead>
          <TableHead>{t('role.remark')}</TableHead>
          <TableHead>{t('common.status')}</TableHead>
          <TableHead class="text-right">{t('common.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#if rows.length === 0}
          <TableRow>
            <TableCell colspan={5} class="py-10 text-center text-muted-foreground">
              {loading ? t('common.loading') : t('common.empty')}
            </TableCell>
          </TableRow>
        {/if}
        {#each rows as role (role.id)}
          <TableRow>
            <TableCell class="font-mono text-sm">{role.code}</TableCell>
            <TableCell class="font-medium">
              {role.name}
              {#if role.code === 'admin'}
                <!-- 内置角色的权限是隐式放行的，界面上要说清楚，
                     否则会有人去找「为什么它的授权是空的」 -->
                <Badge variant="secondary" class="ml-2">{t('role.builtin')}</Badge>
              {/if}
            </TableCell>
            <TableCell class="text-muted-foreground">{role.remark || '—'}</TableCell>
            <TableCell>
              <Badge variant={role.status === 'active' ? 'default' : 'secondary'}>
                {t(role.status === 'active' ? 'common.status.active' : 'common.status.inactive')}
              </Badge>
            </TableCell>
            <TableCell class="text-right whitespace-nowrap">
              {#if session.can('system:role:list')}
                <Button variant="ghost" size="sm" onclick={() => openGrants(role)}>
                  {t('role.grants')}
                </Button>
              {/if}
              {#if session.can('system:role:edit')}
                <Button
                  variant="ghost"
                  size="sm"
                  onclick={() => {
                    editForm = {
                      id: role.id,
                      code: role.code,
                      name: role.name,
                      remark: role.remark,
                      status: role.status,
                    }
                    fieldErrors = []
                    formOpen = true
                  }}
                >
                  {t('common.edit')}
                </Button>
              {/if}
              {#if session.can('system:role:delete')}
                <Button
                  variant="ghost"
                  size="sm"
                  class="text-destructive"
                  disabled={busyID === role.id}
                  onclick={async () => {
                    busyID = role.id
                    await remove(role)
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

  {#if totalPages > 1}
    <div class="flex items-center justify-end gap-2 text-sm">
      <Button variant="outline" size="sm" disabled={page <= 1} onclick={() => goPage(page - 1)}>‹</Button>
      <span class="text-muted-foreground tabular-nums">{page} / {totalPages}</span>
      <Button variant="outline" size="sm" disabled={page >= totalPages} onclick={() => goPage(page + 1)}>›</Button>
    </div>
  {/if}
</div>

<!-- 新建 / 编辑 -->
<Dialog bind:open={formOpen}>
  <DialogContent class="max-w-lg">
    <form onsubmit={save}>
      <DialogHeader>
        <DialogTitle>{t(editForm.id === null ? 'role.createTitle' : 'role.editTitle')}</DialogTitle>
        {#if editForm.id !== null}
          <DialogDescription>{t('role.codeImmutable')}</DialogDescription>
        {/if}
      </DialogHeader>

      <div class="grid gap-4 py-4">
        <div class="grid gap-1.5">
          <Label for="r-code">{t('role.code')}</Label>
          <Input id="r-code" bind:value={editForm.code} disabled={editForm.id !== null} required minlength={2} maxlength={64} />
          {#if fieldErrorOf(fieldErrors, 'code')}<p class="text-sm text-destructive">{fieldErrorOf(fieldErrors, 'code')}</p>{/if}
        </div>
        <div class="grid gap-1.5">
          <Label for="r-name">{t('field.name')}</Label>
          <Input id="r-name" bind:value={editForm.name} required minlength={2} maxlength={128} />
          {#if fieldErrorOf(fieldErrors, 'name')}<p class="text-sm text-destructive">{fieldErrorOf(fieldErrors, 'name')}</p>{/if}
        </div>
        <div class="grid gap-1.5">
          <Label for="r-remark">{t('role.remark')}</Label>
          <Input id="r-remark" bind:value={editForm.remark} maxlength={255} />
        </div>
        <div class="grid gap-1.5">
          <Label for="r-status">{t('common.status')}</Label>
          <NativeSelect id="r-status" class="w-full" bind:value={editForm.status}>
            <option value="active">{t('common.status.active')}</option>
            <option value="inactive">{t('common.status.inactive')}</option>
          </NativeSelect>
        </div>
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onclick={() => (formOpen = false)}>{t('common.cancel')}</Button>
        <Button type="submit" disabled={saving}>{t('common.save')}</Button>
      </DialogFooter>
    </form>
  </DialogContent>
</Dialog>

<!-- 授权 -->
<Dialog open={grantTarget !== null} onOpenChange={(open) => !open && (grantTarget = null)}>
  <DialogContent class="max-h-[85svh] max-w-2xl overflow-y-auto">
    <DialogHeader>
      <DialogTitle>
        {grantTarget ? t('role.grantsTitle', { name: grantTarget.name }) : ''}
      </DialogTitle>
      <DialogDescription>{t('role.grantsHint')}</DialogDescription>
    </DialogHeader>

    {#if grantLoading}
      <p class="py-8 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
    {:else}
      <div class="grid gap-6 py-4">
        <section class="grid gap-2">
          <h3 class="text-sm font-medium">{t('role.menus')}</h3>
          <div class="flex flex-col gap-1.5 rounded-md border p-3">
            {#each menuTree as node (node.id)}
              {#if node.menu_type === 'directory'}
                <label class="flex items-center gap-2 text-sm font-medium">
                  <input
                    type="checkbox"
                    class="size-4 accent-primary"
                    checked={grantMenus.includes(node.id)}
                    onchange={(e) =>
                      (grantMenus = e.currentTarget.checked
                        ? [...grantMenus, node.id]
                        : grantMenus.filter((id) => id !== node.id))}
                  />
                  {tKey(node.title_key)}
                </label>
                <div class="flex flex-col gap-1.5 pl-6">
                  {#each node.children as child (child.id)}
                    <label class="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        class="size-4 accent-primary"
                        checked={grantMenus.includes(child.id)}
                        onchange={(e) =>
                          (grantMenus = e.currentTarget.checked
                            ? [...grantMenus, child.id]
                            : grantMenus.filter((id) => id !== child.id))}
                      />
                      {tKey(child.title_key)}
                    </label>
                  {/each}
                </div>
              {:else}
                <label class="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    class="size-4 accent-primary"
                    checked={grantMenus.includes(node.id)}
                    onchange={(e) =>
                      (grantMenus = e.currentTarget.checked
                        ? [...grantMenus, node.id]
                        : grantMenus.filter((id) => id !== node.id))}
                  />
                  {tKey(node.title_key)}
                </label>
              {/if}
            {/each}
          </div>
        </section>

        <section class="grid gap-2">
          <h3 class="text-sm font-medium">{t('role.perms')}</h3>
          <div class="flex flex-col gap-3 rounded-md border p-3">
            {#each permGroups as group (group.title_key)}
              <div class="grid gap-1.5">
                <p class="text-sm font-medium text-muted-foreground">{tKey(group.title_key)}</p>
                <div class="flex flex-wrap gap-x-4 gap-y-1.5">
                  {#each group.perms as item (item.code)}
                    <label class="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        class="size-4 accent-primary"
                        checked={grantPerms.includes(item.code)}
                        onchange={(e) =>
                          (grantPerms = e.currentTarget.checked
                            ? [...grantPerms, item.code]
                            : grantPerms.filter((c) => c !== item.code))}
                      />
                      {tKey(item.label_key)}
                    </label>
                  {/each}
                </div>
              </div>
            {/each}
          </div>
        </section>
      </div>
    {/if}

    <DialogFooter>
      <Button variant="outline" onclick={() => (grantTarget = null)}>{t('common.cancel')}</Button>
      <Button onclick={saveGrants} disabled={grantSaving || grantLoading}>{t('common.save')}</Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
