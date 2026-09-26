import { enUS } from './en-US'
import { zhCN } from './zh-CN'

export const locales = {
  'zh-CN': '简体中文',
  'en-US': 'English',
} as const

export type Locale = keyof typeof locales
export type MessageKey = keyof typeof zhCN

const dicts: Record<Locale, Record<MessageKey, string>> = {
  'zh-CN': zhCN,
  'en-US': enUS,
}

const STORAGE_KEY = 'mini-ruoyi.locale'

function initialLocale(): Locale {
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved && saved in locales) return saved as Locale
  // 跟随浏览器语言，非中文一律回落英文
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US'
}

// 用 $state 而非普通变量：t() 在模板里读 locale，语言切换才能自动重渲染。
// 模块级 $state 不能直接 export 可变绑定，所以对外只暴露 getter/setter。
let current = $state<Locale>(initialLocale())

function applyDocumentLang(locale: Locale) {
  document.documentElement.lang = locale
}
applyDocumentLang(current)

export function getLocale(): Locale {
  return current
}

export function setLocale(next: Locale) {
  current = next
  localStorage.setItem(STORAGE_KEY, next)
  applyDocumentLang(next)
}

/**
 * 取文案。{name} 形式的占位符会被 params 替换。
 * 未提供的占位符原样保留，便于一眼看出漏传了参数。
 */
export function t(key: MessageKey, params?: Record<string, string | number>): string {
  const template = dicts[current][key] ?? key
  if (!params) return template
  return template.replace(/\{(\w+)\}/g, (_, name: string) =>
    name in params ? String(params[name]) : `{${name}}`,
  )
}

/** 把运行期拼出来的字符串（如 `validation.${rule}`）收窄成 MessageKey。 */
export function hasMessage(key: string): key is MessageKey {
  return key in zhCN
}
