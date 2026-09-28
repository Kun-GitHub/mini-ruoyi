import type { FieldError } from '$lib/api/types'
import { hasMessage, t, type MessageKey } from './index.svelte'

/**
 * 后端可能出现在 errors[].field 里的字段名，与请求结构体的 JSON tag 一一对应
 * （`RegisterJSONFieldNames()` 让 gin 直接报 JSON 名，所以是 snake_case）。
 * 后端新增字段而前端还没补标签时回落到字段名本身，不会显示空白。
 */
const fieldLabelKeys = {
  id: 'field.id',
  name: 'field.name',
  username: 'field.username',
  password: 'field.password',
  old_password: 'field.old_password',
  new_password: 'field.new_password',
  nickname: 'field.nickname',
  mobile: 'field.mobile',
  email: 'field.email',
  status: 'field.status',
  code: 'field.code',
  remark: 'field.remark',
  role_ids: 'field.role_ids',
  parent_id: 'field.parent_id',
  sort: 'field.sort',
  menu_type: 'field.menu_type',
  title_key: 'field.title_key',
  path: 'field.path',
  component: 'field.component',
  icon: 'field.icon',
  cron: 'field.cron',
} satisfies Record<string, MessageKey>

export function fieldLabel(field: string): string {
  const key = (fieldLabelKeys as Record<string, MessageKey | undefined>)[field]
  return key ? t(key) : field
}

/**
 * 把一条字段级校验错误渲染成可展示文案。
 * 后端只给「字段 / 规则 / 参数」，文案在前端字典里拼装。
 */
export function validationText(fe: FieldError): string {
  const candidate = `validation.${fe.rule}`
  const key: MessageKey = hasMessage(candidate) ? candidate : 'validation.default'
  return t(key, {
    field: fieldLabel(fe.field),
    param: fe.param ?? '',
    rule: fe.rule,
  })
}

/**
 * 从错误数组里取某个字段的提示文案，没有就返回 null（模板里直接 `{#if}` 用）。
 *
 * 这个函数存在的唯一理由是「别抄第四遍」：它原本散在 profile / users / roles / menus
 * 四个页面里，其中三份用 `tKey(\`field.${field}\`)` 拼字段标签——`tKey` 查不到键时
 * 原样返回键名，于是未知字段会显示成 `field.xxx`，而不是回落成字段名。
 */
export function fieldErrorOf(errors: FieldError[], field: string): string | null {
  const fe = errors.find((e) => e.field === field)
  return fe ? validationText(fe) : null
}