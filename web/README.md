# mini-ruoyi / web

前端单页应用。Svelte 5 + Vite + TypeScript + Tailwind CSS v4 + shadcn-svelte。

产物是纯静态文件，由后端从磁盘托管（见 [../docs/architecture-web.md](../docs/architecture-web.md)）。
不使用 SvelteKit，没有 SSR，没有 Node 服务端运行时。

## 前置要求

- Node.js `^20.19.0 || >=22.12.0`（Vite 8 的要求）
- 不需要全局安装任何 CLI

## 命令

```bash
npm install          # 安装依赖
npm run dev          # Vite dev server :5173
npm run build        # 生产构建到 dist/
npm run preview      # 本地预览构建产物
npm run check        # svelte-check + tsc，提交前必跑
```

从仓库根目录操作更省事：

```bash
make deps            # = cd web && npm install
make dev-web         # dev server（/api 与 /healthz 代理到 :8080）
make web             # 构建到 web/dist
```

`npm run build` 的产物布局是固定的（`vite.config.ts` 里配置），**不要改**：

```
dist/index.html      由后端在服务根路径返回
dist/assets/*        由后端在 /assets/ 前缀返回，带 immutable 长缓存
```

## 与后端联调

```bash
# 终端 1
make dev-server      # 后端 :8080（web/ 不存在时自动降级为纯 API 模式）

# 终端 2
make dev-web         # Vite :5173，/api 和 /healthz 代理到 :8080
```

`vite.config.ts` 的 `server.proxy` 只代理 `/api` 和 `/healthz`。
新增非 `/api` 前缀的后端路由时，记得同步加代理规则。

## 目录结构

```
web/
├── components.json             # shadcn-svelte 配置
├── index.html                  # Vite 入口
├── vite.config.ts              # 插件、$lib 别名、产物布局、dev 代理
├── tsconfig.json               # $lib 别名（shadcn CLI 读这里）
└── src/
    ├── main.ts                 # mount + vite:preloadError 处理
    ├── app.css                 # Tailwind 入口 + shadcn 主题变量（CLI 生成）
    ├── App.svelte
    └── lib/
        ├── utils.ts            # cn()（CLI 生成）
        ├── i18n/               # 国际化
        └── components/ui/      # shadcn-svelte 组件
```

`$lib` 指向 `src/lib`，别名同时写在 `vite.config.ts` 和两个 tsconfig 里，缺一个都会解析失败。

## 国际化

字典在 `src/lib/i18n/`，目前支持 `zh-CN` 与 `en-US`。手写实现，无运行时库。

### 加一条文案

1. 在 `src/lib/i18n/zh-CN.ts` 加键（它是唯一真源）
2. 在 `src/lib/i18n/en-US.ts` 加对应键——**漏了会编译报错**，不会静默回落
3. 组件里：

```svelte
<script lang="ts">
  import { t } from '$lib/i18n/index.svelte'
</script>

<h1>{t('home.title')}</h1>
<p>{t('validation.min', { field: t('field.name'), param: '2' })}</p>
```

`t()` 读的是模块级 `$state`，所以切换语言时会自动重渲染，不需要刷新页面。

### 加一种语言

1. `src/lib/i18n/xx-XX.ts`，类型标注 `Record<keyof typeof zhCN, string>`
2. 在 `src/lib/i18n/index.svelte.ts` 的 `locales` 与 `dicts` 里各加一项
3. 完成——语言解析会自动出现在切换器里

### 后端错误键的约定

后端只返回 i18n 键（如 `error.notFound`），不返回文案。新增后端错误键时必须同步
`zh-CN.ts`，否则界面上会显示原始的键名。键清单见
[../docs/architecture.md](../docs/architecture.md)。

### 字段级校验错误的渲染

后端返回 `{field, rule, param}`，用 `validationText()` 渲染：

```ts
import { validationText } from '$lib/i18n/errors'

validationText({ field: 'name', rule: 'min', param: '2' })
// → "名称长度不能少于 2 个字符"
```

未知字段回落到字段名本身，未实现的规则回落到 `validation.default`，都不会崩。
字段标签在 `src/lib/i18n/errors.ts` 的 `fieldLabelKeys` 里登记。

## 组件

组件源码在 `src/lib/components/ui/` 下，是**复制进仓库的源码**，不是依赖，可以随意改。

```bash
npx shadcn-svelte@latest add dialog table select
```

主题预设是 `mira`（紧凑，适合密集表格）。换风格：改 `components.json` 的 `style`，
再跑 `npx shadcn-svelte@latest init --reinstall`。

依赖清单与每一项的作用见 [../docs/architecture-web.md](../docs/architecture-web.md)。

## Svelte 5 约定

用 runes，不用旧语法：

| 场景 | 写法 |
| --- | --- |
| 组件内状态 | `let count = $state(0)` |
| 派生值 | `const doubled = $derived(count * 2)` |
| 副作用 | `$effect(() => { ... })` |
| 组件入参 | `let { title } = $props()` |

不装 Pinia / Vuex 那类状态库（runes 就是响应式系统），也不装 axios（用 `fetch`）。

## 尚未实现

| 项 | 说明 |
| --- | --- |
| API 客户端 | `src/lib/api/client.ts`：统一解信封、抛 `ApiError`、401 拦截、CSRF 头 |
| 路由 | 动态路由：后端菜单树 → `import.meta.glob('../pages/**/*.svelte')` |
| 布局 | 侧边栏 + 顶栏 + 多标签页 |
| 登录页 | 依赖后端认证方案落地 |

目前 `App.svelte` 是唯一的页面，只调 `/healthz` 验证前后端链路，**没有任何业务 API 调用**。
