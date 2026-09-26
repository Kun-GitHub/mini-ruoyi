/**
 * 简体中文文案。这是文案的唯一真源：
 * en-US.ts 的类型由它推导，缺键会直接编译报错。
 *
 * 键名与后端 internal/httpx/response.go 里的错误键常量一一对应，
 * 后端新增错误键时必须同步这里。
 */
export const zhCN = {
  'app.name': 'mini-ruoyi',
  'app.description': '一个跑在 1C1G 服务器上的极简管理平台',
  'app.localeLabel': '语言',

  'error.notFound': '资源不存在',
  'error.validationFailed': '参数校验失败',
  'error.bodyTooLarge': '请求体过大',
  'error.malformedBody': '请求体格式错误',
  'error.internal': '服务器内部错误',
  'error.tooManyRequests': '请求过于频繁，请稍后再试',
  'error.serviceUnavailable': '服务暂不可用',
  'error.invalidId': '无效的资源 ID',
  'error.network': '网络异常，请检查连接',

  'validation.required': '{field}不能为空',
  'validation.min': '{field}长度不能少于 {param} 个字符',
  'validation.max': '{field}长度不能超过 {param} 个字符',
  'validation.email': '{field}不是合法的邮箱地址',
  'validation.oneof': '{field}只能是 {param} 之一',
  'validation.default': '{field}不满足 {rule} 规则',

  'field.name': '名称',
  'field.location': '位置',
  'field.enabled': '启用状态',
  'field.id': 'ID',

  'home.backendStatus': '后端连通性',
  'home.status.checking': '检测中',
  'home.status.ok': '正常',
  'home.status.unreachable': '不可达',
  'home.hint': '此页面用于验证 Svelte 5 + Vite + Tailwind v4 + shadcn-svelte 工具链，以及后端从磁盘托管前端产物的链路是否打通。',
} as const
