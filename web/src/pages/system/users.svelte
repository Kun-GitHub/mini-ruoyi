<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api } from '$lib/api/client'
  import { deleteWithConfirm } from '$lib/api/delete'
  import type { FieldError, ImportReport, Page, Role, User, UserDetail } from '$lib/api/types'
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
  import { fieldErrorOf, validationText } from '$lib/i18n/errors'
  import { t, type MessageKey } from '$lib/i18n/index.svelte'
  import { replaceQuery, route } from '$lib/router.svelte'
  import { confirm } from '$lib/stores/confirm.svelte'
  import { notifyError, notifySuccess } from '$lib/stores/notify.svelte'
  import { session } from '$lib/stores/session.svelte'

  const PAGE_SIZE = 20

  /**
   * 分页与筛选是**组件本地状态**，只在挂载时从 URL 读一次。
   *
   * 为什么不持续跟随 URL：多标签页会把打开过的页面同时渲染，而 URL 只反映
   * 当前激活的那个标签。隐藏的页面若持续读全局 URL，切换标签会把它自己的
   * 筛选条件冲掉，还会让它拿着别人的参数重新发请求。
   *
   * 代价是浏览器前进/后退不会回填筛选框。刷新仍然有效——挂载时读一次 URL。
   */
  const initial = new URLSearchParams(route.search)
  let page = $state(Math.max(1, Number(initial.get('page') ?? 1) || 1))
  let filters = $state({
    username: initial.get('username') ?? '',
    nickname: initial.get('nickname') ?? '',
    mobile: initial.get('mobile') ?? '',
    status: initial.get('status') ?? '',
    role_id: initial.get('role_id') ?? '',
  })

  let total = $state(0)
  let rows = $state<User[]>([])
  let loading = $state(false)

  onMount(() => {
    void load()
    // 筛选栏里的角色下拉也要数据。它和编辑弹窗共用 roleOptions，
    // 弹窗打开时还会再拉一次（见 loadRoleOptions 的说明）。
    void loadRoleOptions()
  })

  // 只在有 role:list 权限时才去拉角色列表——否则请求必然 403，
  // 会让一个有 user:add 权限但没有 role:list 的用户看到一堆报错
  const canPickRoles = $derived(session.can('system:role:list'))
  let roleOptions = $state<Role[]>([])
  let roleTotal = $state(0)

  async function load() {
    loading = true
    try {
      const q = new URLSearchParams()
      q.set('page', String(page))
      q.set('page_size', String(PAGE_SIZE))
      for (const key of ['username', 'nickname', 'mobile', 'status', 'role_id'] as const) {
        if (filters[key]) q.set(key, filters[key])
      }
      const res = await api.get<Page<User>>(`/users?${q}`)
      rows = res.list
      total = res.total
      // 服务端会把越界的 page 钳到最后一页，以它为准。
      // 自己留着 page=99 的话，分页器会显示 "99 / 2" 这种不可能的状态
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

  /** 把当前状态反映到地址栏，方便刷新与分享。注意方向是单向的：只写不读。 */
  function syncURL() {
    replaceQuery({ ...filters, page: page === 1 ? undefined : page })
  }

  function search() {
    page = 1
    syncURL()
    void load()
  }

  function resetFilters() {
    filters = { username: '', nickname: '', mobile: '', status: '', role_id: '' }
    page = 1
    syncURL()
    void load()
  }

  function goPage(target: number) {
    page = target
    syncURL()
    void load()
  }

  /**
   * 拉角色候选列表。
   *
   * 在打开弹窗时拉，而不是页面挂载时拉一次：页面在登录后就自动打开了，
   * 之后新建的角色在挂载时还不存在，缓存的列表会让用户找不到刚建的角色。
   */
  async function loadRoleOptions() {
    if (!canPickRoles) return
    try {
      const res = await api.get<Page<Role>>('/roles?page_size=100')
      roleOptions = res.list
      roleTotal = res.total
    } catch {
      roleOptions = []
      roleTotal = 0
    }
  }

  // 后端的键是字符串，这里收窄成 MessageKey：它最后总是喂给 t() 或 notifyError()，
  // 收窄一次比在每个调用点 as 一次要好
  function errKey(err: unknown): MessageKey {
    return err instanceof ApiError ? (err.key as MessageKey) : 'error.internal'
  }

  // ---- 导入 / 导出 ----

  /**
   * 导出链接用 `Button` 的 href 分支（渲染成 `<a>`），而不是按钮 + onclick。
   *
   * 导出是浏览器的下载行为：交给它自己处理进度、保存位置与失败重试。
   * 用 fetch 拿 blob 再手工造链接要多写一堆代码，还会把整份文件读进内存。
   *
   * 筛选条件必须带上：导出的应该是屏幕上看到的那些行，不是全表。
   */
  const exportHref = $derived.by(() => {
    const q = new URLSearchParams()
    for (const key of ['username', 'nickname', 'mobile', 'status', 'role_id'] as const) {
      if (filters[key]) q.set(key, filters[key])
    }
    const qs = q.toString()
    return `/api/v1/users/export${qs ? `?${qs}` : ''}`
  })

  /** 报告里最多列出的错误条数，超过的部分只报个数。 */
  const IMPORT_ERROR_LIMIT = 20

  let importOpen = $state(false)
  let importFile = $state<File | null>(null)
  let importPassword = $state('')
  let importReport = $state<ImportReport | null>(null)
  let importError = $state<MessageKey | null>(null)
  let importing = $state(false)

  /**
   * 两个按钮都能发出校验请求；「确认导入」只在「校验过、没有不合格行、且确实有可导行」时可点。
   *
   * 一行不合格就一行都不写，所以这里不是「跳过坏行」而是「全部退回」。
   */
  const importReady = $derived(
    importReport !== null && importReport.errors.length === 0 && importReport.valid > 0,
  )
  const importShownErrors = $derived(importReport?.errors.slice(0, IMPORT_ERROR_LIMIT) ?? [])

  function openImport() {
    importFile = null
    importPassword = ''
    importReport = null
    importError = null
    importOpen = true
  }

  function pickImportFile(event: Event) {
    importFile = (event.currentTarget as HTMLInputElement).files?.[0] ?? null
    // 换了文件，上一次的校验结果就不再对应当前内容，先清掉
    importReport = null
    importError = null
  }

  /**
   * 下载空模板。
   *
   * 在前端生成而不是从后端取：模板就是一行表头加一行示例，为它加一个接口意味着
   * 多一条要么公开、要么再配权限码的路由，而公开端点是刻意限制的。
   *
   * BOM 不能少：缺了它用 Excel 打开 UTF-8 的 CSV 会把中文显示成乱码。
   * 列名要和后端认得的列一致，改这里就得回头改 handler/user_handler.go。
   */
  function downloadTemplate() {
    const csv =
      '\ufeffusername,nickname,mobile,email,status\nzhangsan,张三,13800000000,zhangsan@example.com,active\n'
    const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }))
    const a = document.createElement('a')
    a.href = url
    a.download = 'users-template.csv'
    a.click()
    URL.revokeObjectURL(url)
  }

  /**
   * 上传并校验；dryRun 走 `?dry_run=1`，只查不写，报告与真导入同形。
   *
   * 密码随表单整批传一次，不写进 CSV：明文密码不该落在会被转发的文件里。
   */
  async function submitImport(dryRun: boolean) {
    if (!importFile) return
    importing = true
    importError = null
    try {
      const body = new FormData()
      body.append('password', importPassword)
      body.append('file', importFile)
      importReport = await api.upload<ImportReport>(`/users/import?dry_run=${dryRun ? 1 : 0}`, body)
      if (!dryRun && importReport.imported > 0) {
        notifySuccess('notify.imported', { count: importReport.imported })
        importOpen = false
        await load()
      }
    } catch (err) {
      importReport = null
      importError = errKey(err)
    } finally {
      importing = false
    }
  }

  // ---- 新建 / 编辑 ----

  type Form = {
    id: number | null
    username: string
    password: string
    nickname: string
    mobile: string
    email: string
    status: 'active' | 'inactive'
    roleIDs: number[]
  }

  const emptyForm = (): Form => ({
    id: null,
    username: '',
    password: '',
    nickname: '',
    mobile: '',
    email: '',
    status: 'active',
    roleIDs: [],
  })

  let form = $state<Form>(emptyForm())
  let formOpen = $state(false)
  let saving = $state(false)
  let fieldErrors = $state<FieldError[]>([])

  function openCreate() {
    form = emptyForm()
    fieldErrors = []
    formOpen = true
    void loadRoleOptions()
  }

  async function openEdit(id: number) {
    fieldErrors = []
    void loadRoleOptions()
    try {
      const detail = await api.get<UserDetail>(`/users/${id}`)
      form = {
        id: detail.id,
        username: detail.username,
        password: '',
        nickname: detail.nickname,
        mobile: detail.mobile,
        email: detail.email,
        status: detail.status,
        roleIDs: detail.role_ids,
      }
      formOpen = true
    } catch (err) {
      notifyError(errKey(err))
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault()
    saving = true
    fieldErrors = []
    try {
      let userId: number

      if (form.id === null) {
        // 后端返回创建后的完整实体，角色绑定需要它的 id
        const created = await api.post<User>('/users', {
          username: form.username,
          password: form.password,
          nickname: form.nickname,
          mobile: form.mobile,
          email: form.email,
          status: form.status,
        })
        userId = created.id
      } else {
        // username 不可变，后端也不接受它
        await api.put(`/users/${form.id}`, {
          nickname: form.nickname,
          mobile: form.mobile,
          email: form.email,
          status: form.status,
        })
        userId = form.id
      }

      // 角色是在同一个弹窗里选的，**新建时也要绑**。
      // 之前这里写成 `if (form.id !== null)`，结果是新建时选的角色被静默丢弃，
      // 既不报错也不生效——用户以为建好了，实际没有角色。
      //
      // 新建且没勾任何角色时跳过：这时没有任何东西要删，
      // 发一个空的绑定请求是纯粹的浪费（每次建用户都多一次往返）。
      const nothingToBind = form.id === null && form.roleIDs.length === 0
      if (canPickRoles && !nothingToBind) {
        await api.put(`/users/${userId}/roles`, { role_ids: form.roleIDs })
      }

      notifySuccess('notify.saved')
      formOpen = false
      await load()
    } catch (err) {
      if (err instanceof ApiError) {
        fieldErrors = err.fieldErrors
        // 字段级错误已经挂在输入框上了，不要再弹一个重复的通知
        if (err.fieldErrors.length === 0) notifyError(err.key)
      } else {
        notifyError('error.internal')
      }
    } finally {
      saving = false
    }
  }

  // ---- 重置密码 ----

  let pwTarget = $state<User | null>(null)
  let pwValue = $state('')
  let pwErrors = $state<FieldError[]>([])

  async function resetPassword(event: SubmitEvent) {
    event.preventDefault()
    if (!pwTarget) return
    pwErrors = []
    try {
      await api.put(`/users/${pwTarget.id}/password`, { password: pwValue })
      notifySuccess('notify.passwordReset')
      pwTarget = null
      pwValue = ''
    } catch (err) {
      if (err instanceof ApiError) {
        pwErrors = err.fieldErrors
        if (err.fieldErrors.length === 0) notifyError(err.key)
      }
    }
  }

  // ---- 删除 ----

  async function remove(user: User) {
    try {
      const done = await deleteWithConfirm(`/users/${user.id}`)
      if (!done) return
      notifySuccess('notify.deleted')
      // 删掉本页最后一条时要往前翻一页，否则会停在一个空白页
      if (rows.length === 1 && page > 1) goPage(page - 1)
      else await load()
    } catch (err) {
      notifyError(errKey(err))
    }
  }

  // ---- 强制下线 ----

  /**
   * 踢掉该用户的**全部**会话。
   *
   * 后端的 `/users/:id/sessions` 早就存在（重置密码也走它），这里补的是入口：
   * 账号被盗时要做的正是「把这个人的所有会话一起踢掉」，而在会话列表里只能一条条踢，
   * 会话超过 20 条还得翻页找全。
   */
  async function kickAll(user: User) {
    const ok = await confirm({
      titleKey: 'confirm.kickTitle',
      bodyKey: 'session.kickAllConfirm',
      details: [{ labelKey: 'field.username', value: user.username }],
      danger: true,
    })
    if (!ok) return

    busyID = user.id
    try {
      await api.del(`/users/${user.id}/sessions`)
      notifySuccess('notify.kicked')
      await load()
    } catch (err) {
      notifyError(errKey(err))
    } finally {
      busyID = null
    }
  }

  let busyID = $state<number | null>(null)
  const totalPages = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)))
</script>

<div class="flex flex-col gap-4">
  <!-- 筛选栏。
       移动端折成两列，桌面端一行放下——后台主要是桌面用，不为窄屏做抽屉。 -->
  <div data-testid="filters" class="rounded-lg border bg-card p-4">
    <div class="flex flex-wrap items-end gap-3">
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('user.username')}</span>
        <Input bind:value={filters.username} onkeydown={(e) => e.key === 'Enter' && search()} />
      </label>
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('user.nickname')}</span>
        <Input bind:value={filters.nickname} onkeydown={(e) => e.key === 'Enter' && search()} />
      </label>
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('user.mobile')}</span>
        <Input bind:value={filters.mobile} onkeydown={(e) => e.key === 'Enter' && search()} />
      </label>
      <label class="flex w-48 flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">{t('common.status')}</span>
        <NativeSelect class="w-full" bind:value={filters.status}>
          <option value="">{t('common.all')}</option>
          <option value="active">{t('common.status.active')}</option>
          <option value="inactive">{t('common.status.inactive')}</option>
        </NativeSelect>
      </label>
      <!-- 没 role:list 权限就拉不到角色列表，那这个下拉只能是空的；
           与其摆一个永远没有选项的框，不如不显示 -->
      {#if canPickRoles}
        <label class="flex w-48 flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">{t('user.roles')}</span>
          <NativeSelect class="w-full" bind:value={filters.role_id}>
            <option value="">{t('common.all')}</option>
            {#each roleOptions as role (role.id)}
              <option value={String(role.id)}>{role.name}</option>
            {/each}
          </NativeSelect>
        </label>
      {/if}

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
    <div class="flex items-center gap-2">
      <Button data-testid="export-users" variant="outline" size="sm" href={exportHref}>
        {t('common.export')}
      </Button>
      {#if session.can('system:user:add')}
        <Button data-testid="import-open" variant="outline" size="sm" onclick={openImport}>
          {t('common.import')}
        </Button>
        <Button size="sm" onclick={openCreate}>{t('common.create')}</Button>
      {/if}
    </div>
  </div>

  <div class="rounded-lg border bg-card">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('user.username')}</TableHead>
          <TableHead>{t('user.nickname')}</TableHead>
          <TableHead>{t('user.mobile')}</TableHead>
          <TableHead>{t('common.status')}</TableHead>
          <TableHead>{t('user.loginAt')}</TableHead>
          <TableHead class="text-right">{t('common.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#if rows.length === 0}
          <TableRow>
            <TableCell colspan={6} class="py-10 text-center text-muted-foreground">
              {loading ? t('common.loading') : t('common.empty')}
            </TableCell>
          </TableRow>
        {/if}
        {#each rows as user (user.id)}
          <TableRow>
            <TableCell class="font-medium">{user.username}</TableCell>
            <TableCell>{user.nickname || '—'}</TableCell>
            <TableCell>{user.mobile || '—'}</TableCell>
            <TableCell>
              <Badge variant={user.status === 'active' ? 'default' : 'secondary'}>
                {t(user.status === 'active' ? 'common.status.active' : 'common.status.inactive')}
              </Badge>
            </TableCell>
            <TableCell class="text-muted-foreground">
              {user.login_at ? new Date(user.login_at).toLocaleString() : t('common.never')}
            </TableCell>
            <TableCell class="text-right whitespace-nowrap">
              {#if session.can('system:user:edit')}
                <Button variant="ghost" size="sm" onclick={() => openEdit(user.id)}>
                  {t('common.edit')}
                </Button>
              {/if}
              {#if session.can('system:user:resetPwd')}
                <Button
                  variant="ghost"
                  size="sm"
                  onclick={() => {
                    pwTarget = user
                    pwValue = ''
                    pwErrors = []
                  }}
                >
                  {t('user.resetPassword')}
                </Button>
              {/if}
              {#if session.can('monitor:session:kick')}
                <Button
                  variant="ghost"
                  size="sm"
                  class="text-destructive"
                  disabled={user.id === session.user?.id || busyID === user.id}
                  onclick={() => kickAll(user)}
                >
                  {t('session.kickUser')}
                </Button>
              {/if}
              {#if session.can('system:user:delete')}
                <Button
                  variant="ghost"
                  size="sm"
                  class="text-destructive"
                  disabled={busyID === user.id}
                  onclick={async () => {
                    busyID = user.id
                    await remove(user)
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
      <Button variant="outline" size="sm" disabled={page <= 1} onclick={() => goPage(page - 1)}>
        ‹
      </Button>
      <span class="text-muted-foreground tabular-nums">{page} / {totalPages}</span>
      <Button variant="outline" size="sm" disabled={page >= totalPages} onclick={() => goPage(page + 1)}>
        ›
      </Button>
    </div>
  {/if}
</div>

<!-- 新建 / 编辑 -->
<Dialog bind:open={formOpen}>
  <DialogContent class="max-w-lg">
    <form onsubmit={save}>
      <DialogHeader>
        <DialogTitle>
          {t(form.id === null ? 'user.createTitle' : 'user.editTitle')}
        </DialogTitle>
        {#if form.id !== null}
          <DialogDescription>{t('user.usernameImmutable')}</DialogDescription>
        {/if}
      </DialogHeader>

      <div class="grid gap-4 py-4">
        <div class="grid gap-1.5">
          <Label for="u-username">{t('user.username')}</Label>
          <Input
            id="u-username"
            bind:value={form.username}
            disabled={form.id !== null}
            required
            minlength={2}
            maxlength={128}
          />
          {#if fieldErrorOf(fieldErrors, 'username')}
            <p class="text-sm text-destructive">{fieldErrorOf(fieldErrors, 'username')}</p>
          {/if}
        </div>

        {#if form.id === null}
          <div class="grid gap-1.5">
            <Label for="u-password">{t('user.password')}</Label>
            <Input
              id="u-password"
              type="password"
              bind:value={form.password}
              required
              minlength={8}
              maxlength={72}
            />
            {#if fieldErrorOf(fieldErrors, 'password')}
              <p class="text-sm text-destructive">{fieldErrorOf(fieldErrors, 'password')}</p>
            {/if}
          </div>
        {/if}

        <div class="grid gap-1.5">
          <Label for="u-nickname">{t('user.nickname')}</Label>
          <Input id="u-nickname" bind:value={form.nickname} maxlength={128} />
        </div>

        <div class="grid grid-cols-2 gap-4">
          <div class="grid gap-1.5">
            <Label for="u-mobile">{t('user.mobile')}</Label>
            <Input id="u-mobile" bind:value={form.mobile} maxlength={20} />
          </div>
          <div class="grid gap-1.5">
            <Label for="u-email">{t('user.email')}</Label>
            <Input id="u-email" type="email" bind:value={form.email} maxlength={64} />
            {#if fieldErrorOf(fieldErrors, 'email')}
              <p class="text-sm text-destructive">{fieldErrorOf(fieldErrors, 'email')}</p>
            {/if}
          </div>
        </div>

        <div class="grid gap-1.5">
          <Label for="u-status">{t('common.status')}</Label>
          <!-- 原生 select：只有两个取值，为它引入一套 headless 组件要多付约 20 kB gzip，
               而原生元素自带键盘导航、移动端选择和读屏支持 -->
          <NativeSelect
            id="u-status"
            class="w-full"
            bind:value={form.status}
          >
            <option value="active">{t('common.status.active')}</option>
            <option value="inactive">{t('common.status.inactive')}</option>
          </NativeSelect>
        </div>

        {#if canPickRoles}
          <div class="grid gap-2">
            <Label>{t('user.roles')}</Label>
            {#if roleTotal > roleOptions.length}
              <!-- 一次最多拉 100 个。超出时明说，别让人以为「列表里没有就是没有这个角色」 -->
              <p class="text-xs text-muted-foreground">
                {t('user.rolesTruncated', { shown: roleOptions.length, total: roleTotal })}
              </p>
            {/if}
            <div class="flex flex-wrap gap-3">
              {#each roleOptions as role (role.id)}
                <label class="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    class="size-4 accent-primary"
                    checked={form.roleIDs.includes(role.id)}
                    onchange={(e) => {
                      form.roleIDs = e.currentTarget.checked
                        ? [...form.roleIDs, role.id]
                        : form.roleIDs.filter((id) => id !== role.id)
                    }}
                  />
                  {role.name}
                </label>
              {/each}
            </div>
          </div>
        {/if}
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onclick={() => (formOpen = false)}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" disabled={saving}>{t('common.save')}</Button>
      </DialogFooter>
    </form>
  </DialogContent>
</Dialog>

<!-- 导入用户 -->
<Dialog bind:open={importOpen}>
  <DialogContent class="max-w-lg">
    <DialogHeader>
      <DialogTitle>{t('user.importTitle')}</DialogTitle>
      <DialogDescription>{t('user.importHint')}</DialogDescription>
    </DialogHeader>

    <div class="grid gap-4">
      <div class="grid gap-1.5">
        <Label for="imp-password">{t('user.password')}</Label>
        <Input
          id="imp-password"
          type="password"
          required
          minlength={8}
          maxlength={72}
          bind:value={importPassword}
        />
        <p class="text-xs text-muted-foreground">{t('user.importPasswordHint')}</p>
      </div>

      <div class="grid gap-1.5">
        <div class="flex items-center justify-between">
          <Label for="imp-file">{t('user.importFile')}</Label>
          <Button type="button" variant="outline" size="sm" onclick={downloadTemplate}>
            {t('user.importTemplate')}
          </Button>
        </div>
        <input
          id="imp-file"
          data-testid="import-file"
          type="file"
          accept=".csv,text/csv"
          class="cursor-pointer"
          disabled={importing}
          onchange={pickImportFile}
        />
      </div>

      {#if importError}
        <p data-testid="import-error" class="text-sm text-destructive">{t(importError)}</p>
      {/if}

      {#if importReport}
        <div data-testid="import-report" class="grid gap-2 rounded-md border p-3 text-sm">
          <p>
            {t('user.importReport', {
              total: importReport.total,
              valid: importReport.valid,
              imported: importReport.imported,
            })}
          </p>
          {#if importReport.errors.length > 0}
            <p class="text-destructive">
              {t('user.importBlocked', { count: importReport.errors.length })}
            </p>
            <ul class="grid gap-1">
              {#each importShownErrors as err}
                <!-- 行号在前，因为修文件时先要找到那一行 -->
                <li class="text-xs text-muted-foreground">
                  {t('user.importRow', { row: err.row })}：{validationText(err)}
                </li>
              {/each}
            </ul>
            {#if importReport.errors.length > importShownErrors.length}
              <p class="text-xs text-muted-foreground">
                {t('user.importMoreErrors', {
                  count: importReport.errors.length - importShownErrors.length,
                })}
              </p>
            {/if}
          {:else if importReport.valid > 0}
            <p class="text-muted-foreground">{t('user.importReady')}</p>
          {/if}
        </div>
      {/if}
    </div>

    <DialogFooter>
      <Button type="button" variant="outline" onclick={() => (importOpen = false)}>
        {t('common.cancel')}
      </Button>
      <Button
        data-testid="import-check"
        type="button"
        variant="outline"
        disabled={importing || !importFile || importPassword.length < 8}
        onclick={() => submitImport(true)}
      >
        {t('user.importCheck')}
      </Button>
      <Button
        data-testid="import-confirm"
        type="button"
        disabled={importing || !importReady}
        onclick={() => submitImport(false)}
      >
        {t('user.importConfirm')}
      </Button>
    </DialogFooter>
  </DialogContent>
</Dialog>

<!-- 重置密码 -->
<Dialog open={pwTarget !== null} onOpenChange={(open) => !open && (pwTarget = null)}>
  <DialogContent class="max-w-md">
    <form onsubmit={resetPassword}>
      <DialogHeader>
        <DialogTitle>{t('user.resetPassword')}</DialogTitle>
        <DialogDescription>{t('user.resetPasswordHint')}</DialogDescription>
      </DialogHeader>

      <div class="grid gap-1.5 py-4">
        <Label for="pw">{t('user.password')}</Label>
        <Input
          id="pw"
          type="password"
          bind:value={pwValue}
          required
          minlength={8}
          maxlength={72}
        />
        {#if pwErrors.length > 0}
          <p class="text-sm text-destructive">{fieldErrorOf(pwErrors, 'password')}</p>
        {/if}
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onclick={() => (pwTarget = null)}>
          {t('common.cancel')}
        </Button>
        <Button type="submit">{t('common.confirm')}</Button>
      </DialogFooter>
    </form>
  </DialogContent>
</Dialog>
