<script lang="ts">
  import { ApiError, api } from '$lib/api/client'
  import type { FieldError } from '$lib/api/types'
  import { Button } from '$lib/components/ui/button'
  import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { t, tKey } from '$lib/i18n/index.svelte'
  import { notifyError, notifySuccess } from '$lib/stores/notify.svelte'
  import { bootstrap, session } from '$lib/stores/session.svelte'

  // 用户名只读，直接从会话取；编辑字段见下面的 initial 快照
  const user = $derived(session.user)

  function errKey(err: unknown): string {
    return err instanceof ApiError ? err.key : 'error.internal'
  }

  /** 把字段级校验错误渲染成文案。后端只给 field/rule/param。 */
  function fieldError(errors: FieldError[], field: string): string | null {
    const fe = errors.find((e) => e.field === field)
    if (!fe) return null
    return tKey(`validation.${fe.rule}`, {
      field: tKey(`field.${field}`),
      param: fe.param ?? '',
      rule: fe.rule,
    })
  }

  // ---- 基本资料 ----

  // 表单是**编辑缓冲区**，只在挂载时取一次初始值：
  // 用 $derived 的话，保存成功后 bootstrap() 刷新会话会把用户正在输入的内容覆盖掉。
  // 这里先取一个非响应式快照，把「只要初始值」这件事写在明处。
  const initial = session.user

  let nickname = $state(initial?.nickname ?? '')
  let mobile = $state(initial?.mobile ?? '')
  let email = $state(initial?.email ?? '')
  let savingProfile = $state(false)
  let profileErrors = $state<FieldError[]>([])

  async function saveProfile(event: SubmitEvent) {
    event.preventDefault()
    savingProfile = true
    profileErrors = []
    try {
      await api.put('/profile', { nickname, mobile, email })
      // 重新拉一次会话：顶栏显示的是会话里的昵称，不刷新就还是旧值
      await bootstrap()
      notifySuccess('notify.saved')
    } catch (err) {
      if (err instanceof ApiError) {
        profileErrors = err.fieldErrors
        if (err.fieldErrors.length === 0) notifyError(err.key)
      } else {
        notifyError('error.internal')
      }
    } finally {
      savingProfile = false
    }
  }

  // ---- 修改密码 ----

  let oldPassword = $state('')
  let newPassword = $state('')
  let confirmPassword = $state('')
  let changingPassword = $state(false)
  let passwordErrors = $state<FieldError[]>([])
  let mismatch = $state(false)

  async function changePassword(event: SubmitEvent) {
    event.preventDefault()
    passwordErrors = []
    mismatch = false

    // 两次输入一致性在前端挡一下：这个规则只在界面上有意义，
    // 后端收到的只有一个新密码
    if (newPassword !== confirmPassword) {
      mismatch = true
      return
    }

    changingPassword = true
    try {
      await api.put('/profile/password', {
        old_password: oldPassword,
        new_password: newPassword,
      })
      notifySuccess('profile.passwordChanged')
      // 清空表单，避免密码留在输入框里
      oldPassword = ''
      newPassword = ''
      confirmPassword = ''
    } catch (err) {
      if (err instanceof ApiError) {
        passwordErrors = err.fieldErrors
        if (err.fieldErrors.length === 0) notifyError(err.key)
      } else {
        notifyError('error.internal')
      }
    } finally {
      changingPassword = false
    }
  }
</script>

<div class="flex flex-col gap-4">
  <p class="text-sm text-muted-foreground">{t('profile.noPermissionHint')}</p>

  <div class="grid gap-4 lg:grid-cols-2">
    <Card>
      <CardHeader>
        <CardTitle>{t('profile.basic')}</CardTitle>
        <CardDescription>{t('profile.usernameHint')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form class="flex flex-col gap-4" onsubmit={saveProfile}>
          <div class="grid gap-1.5">
            <Label for="p-username">{t('user.username')}</Label>
            <!-- 用户名只读：它是身份标识，审计日志靠它定位到人 -->
            <Input id="p-username" value={user?.username ?? ''} disabled />
          </div>
          <div class="grid gap-1.5">
            <Label for="p-nickname">{t('user.nickname')}</Label>
            <Input id="p-nickname" bind:value={nickname} maxlength={128} />
            {#if fieldError(profileErrors, 'nickname')}
              <p class="text-sm text-destructive">{fieldError(profileErrors, 'nickname')}</p>
            {/if}
          </div>
          <div class="grid gap-1.5">
            <Label for="p-mobile">{t('user.mobile')}</Label>
            <Input id="p-mobile" bind:value={mobile} maxlength={20} />
            {#if fieldError(profileErrors, 'mobile')}
              <p class="text-sm text-destructive">{fieldError(profileErrors, 'mobile')}</p>
            {/if}
          </div>
          <div class="grid gap-1.5">
            <Label for="p-email">{t('user.email')}</Label>
            <Input id="p-email" type="email" bind:value={email} maxlength={64} />
            {#if fieldError(profileErrors, 'email')}
              <p class="text-sm text-destructive">{fieldError(profileErrors, 'email')}</p>
            {/if}
          </div>
          <div class="flex justify-end">
            <Button type="submit" disabled={savingProfile}>{t('common.save')}</Button>
          </div>
        </form>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <CardTitle>{t('profile.password')}</CardTitle>
        <CardDescription>{t('profile.passwordChanged')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form class="flex flex-col gap-4" onsubmit={changePassword}>
          <div class="grid gap-1.5">
            <Label for="p-old">{t('profile.oldPassword')}</Label>
            <Input id="p-old" type="password" bind:value={oldPassword} required maxlength={72} />
            {#if fieldError(passwordErrors, 'old_password')}
              <p class="text-sm text-destructive">{fieldError(passwordErrors, 'old_password')}</p>
            {/if}
          </div>
          <div class="grid gap-1.5">
            <Label for="p-new">{t('profile.newPassword')}</Label>
            <Input
              id="p-new"
              type="password"
              bind:value={newPassword}
              required
              minlength={8}
              maxlength={72}
            />
            {#if fieldError(passwordErrors, 'new_password')}
              <p class="text-sm text-destructive">{fieldError(passwordErrors, 'new_password')}</p>
            {/if}
          </div>
          <div class="grid gap-1.5">
            <Label for="p-confirm">{t('profile.confirmPassword')}</Label>
            <Input
              id="p-confirm"
              type="password"
              bind:value={confirmPassword}
              required
              minlength={8}
              maxlength={72}
            />
            {#if mismatch}
              <p class="text-sm text-destructive">{t('profile.passwordMismatch')}</p>
            {/if}
          </div>
          <div class="flex justify-end">
            <Button type="submit" disabled={changingPassword}>{t('common.save')}</Button>
          </div>
        </form>
      </CardContent>
    </Card>
  </div>
</div>
