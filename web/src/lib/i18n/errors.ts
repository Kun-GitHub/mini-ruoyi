import { hasMessage, t, type MessageKey } from './index.svelte'

/** 字段级校验错误，对应后端 internal/httpx.FieldError */
export type FieldError = {
  field: string
  rule: string
  param?: string
}

/**
 * 已知字段的标签。后端新增字段而前端还没补标签时回落到字段名本身，
 * 不会显示空白。
 */
/** 后端可能出现在 errors[].field 里的字段名，与 docs/schema.md 的列名一一对应。 */
const fieldLabelKeys = {
  id: 'field.id',
  name: 'field.name',
  username: 'field.username',
  password: 'field.password',
  nickname: 'field.nickname',
  mobile: 'field.mobile',
  email: 'field.email',
  status: 'field.status',
  code: 'field.code',
  remark: 'field.remark',
  parent_id: 'field.parent_id',
  sort: 'field.sort',
  menu_type: 'field.menu_type',
  title_key: 'field.title_key',
  path: 'field.path',
  component: 'field.component',
  icon: 'field.icon',
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
