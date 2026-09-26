import type { zhCN } from './zh-CN'

/**
 * English messages.
 *
 * The type is derived from zh-CN, so a missing or misspelled key is a compile error
 * rather than a silently untranslated string at runtime.
 */
export const enUS: Record<keyof typeof zhCN, string> = {
  'app.name': 'mini-ruoyi',
  'app.description': 'A minimal admin platform that runs on a 1C1G server',
  'app.localeLabel': 'Language',

  'error.notFound': 'Resource not found',
  'error.validationFailed': 'Validation failed',
  'error.bodyTooLarge': 'Request body too large',
  'error.malformedBody': 'Malformed request body',
  'error.internal': 'Internal server error',
  'error.tooManyRequests': 'Too many requests, please try again later',
  'error.serviceUnavailable': 'Service unavailable',
  'error.invalidId': 'Invalid resource ID',
  'error.network': 'Network error, please check your connection',

  'validation.required': '{field} is required',
  'validation.min': '{field} must be at least {param} characters',
  'validation.max': '{field} must be at most {param} characters',
  'validation.email': '{field} is not a valid email address',
  'validation.oneof': '{field} must be one of {param}',
  'validation.default': '{field} does not satisfy the {rule} rule',

  'field.name': 'Name',
  'field.location': 'Location',
  'field.enabled': 'Enabled',
  'field.id': 'ID',

  'home.backendStatus': 'Backend connectivity',
  'home.status.checking': 'Checking',
  'home.status.ok': 'Healthy',
  'home.status.unreachable': 'Unreachable',
  'home.hint': 'This page verifies the Svelte 5 + Vite + Tailwind v4 + shadcn-svelte toolchain, and that the backend serves the frontend build from disk.',
}
