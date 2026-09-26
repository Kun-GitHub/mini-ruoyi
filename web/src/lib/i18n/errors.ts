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
const fieldLabelKeys = {
  id: 'field.id',
  name: 'field.name',
  location: 'field.location',
  enabled: 'field.enabled',
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
