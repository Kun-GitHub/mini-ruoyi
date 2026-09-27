# 前端架构

Svelte 5 + Vite + TypeScript + Tailwind CSS v4 + shadcn-svelte。
纯静态单页应用，产物由后端从磁盘托管，**不使用 SvelteKit**。

## 1. 技术选型

| 项 | 版本 | 说明 |
| --- | --- | --- |
| Svelte | 5.57 | 用 runes（`$state` / `$derived` / `$props`），不用旧式 `export let` / store |
| Vite | 8.3 | 构建与 dev server |
| TypeScript | 6.0 | 全量 TS，含 `.svelte` 内的类型检查 |
| Tailwind CSS | 4.3 | v4 的 CSS-first 配置，无 `tailwind.config.js` |
| shadcn-svelte | 1.7 | 组件**复制进仓库**，不是运行时依赖 |
| bits-ui | 2.19 | shadcn-svelte 的无头组件底座 |
| lucide | — | 图标库（`components.json` 声明） |

### 为什么不用 SvelteKit

我们要的是「静态产物 + 后端托管」。SvelteKit 的 SSR、Node adapter 在这个形态下全是负担；
用 `adapter-static` + `ssr=false` 又只是把复杂度加回来。直接 Vite + Svelte，路由自己写。

### 为什么组件用 shadcn-svelte

组件源码复制进 `src/lib/components/ui/`，**不是依赖包**。因此：

- 只打包用到的组件，不存在"引入一个库导致全量进包"的问题
- 组件可任意改样式，不受上游 API 约束
- 无版本升级破坏性变更的风险

代价是组件不会自动获得上游修复，需要手动 `npx shadcn-svelte add <组件> --overwrite` 更新。

## 2. 目录结构

```
web/
├── components.json             # shadcn-svelte 配置（style=mira, baseColor=neutral, iconLibrary=lucide）
├── index.html                  # Vite 入口，lang="zh-CN"
├── vite.config.ts              # 插件、别名、构建产物布局、dev 代理
├── tsconfig.json               # 含 $lib 路径别名（shadcn CLI 读这里）
└── src/
    ├── main.ts                 # mount + vite:preloadError 处理
    ├── app.css                 # Tailwind 入口 + shadcn 主题变量（由 CLI 生成）
    ├── App.svelte              # 按会话状态在 启动中 / 登录页 / 应用外壳 之间切换
    ├── pages/                  # 业务页面，路径与菜单的 component 字段一一对应
    │   └── system/
    │       ├── users.svelte
    │       ├── roles.svelte
    │       ├── menus.svelte
    │       └── apis.svelte     # 只读：权限码 ↔ 接口（为什么不只读见 schema.md §8.4）
    └── lib/
        ├── api/
        │   ├── client.ts       # 信封解包、ApiError、CSRF 注入、401 回调
        │   ├── types.ts        # 与后端 DTO 一一对应的类型
        │   └── delete.ts       # 「409 → 确认框 → cascade 重发」的封装
        ├── components/
        │   ├── app-shell.svelte    # 侧边栏 + 顶栏 + 动态页面渲染
        │   ├── confirm-host.svelte # 全局确认框
        │   ├── toaster.svelte      # 全局通知
        │   ├── login-form.svelte
        │   └── ui/                 # shadcn-svelte 组件
        ├── i18n/               # 见 §4
        ├── stores/             # session / notify / confirm（都是 .svelte.ts）
        ├── router.svelte.ts    # 极简路由 + 菜单驱动的页面解析
        └── utils.ts            # cn() 与 Svelte 组件类型工具（由 CLI 生成）
```

`$lib` 别名指向 `src/lib`，同时声明在 `vite.config.ts`（Vite 解析）和
`tsconfig.json` / `tsconfig.app.json`（TS 解析与 shadcn CLI 校验）里，两边都不能漏。

> `tsconfig.json` 里**不要**加 `baseUrl`。TS 4.4 起 `paths` 独立可用，而 `baseUrl` 在 TS 7.0 会被移除
> （`svelte-check` 会报 deprecation 警告）。

## 3. Svelte 5 响应式约定

用 runes，不用旧语法。

| 场景 | 写法 |
| --- | --- |
| 组件内部状态 | `let count = $state(0)` |
| 派生值 | `const doubled = $derived(count * 2)` |
| 副作用 | `$effect(() => { ... })` |
| 组件入参 | `let { title, children } = $props()` |
| 双向绑定到组件 | `bind:value` + `$bindable()` |

**状态管理不装库。** runes 本身就是响应式系统，Vue 的 Pinia / Vuex 那套在这里没有对应需求。
跨组件共享状态用 `.svelte.ts` 模块导出 getter/setter（见 §4 的 i18n 实现）。

**HTTP 不装 axios。** 原生 `fetch` + 一层薄封装即可，见 §5。

## 4. i18n

手写实现，约 110 行，**零运行时依赖**。

```
src/lib/i18n/
├── zh-CN.ts          # 唯一真源，as const
├── en-US.ts          # Record<keyof typeof zhCN, string> → 缺键是编译错误
├── index.svelte.ts   # 语言状态（$state）+ t() + 持久化
└── errors.ts         # 字段标签 + validationText()
```

### 4.1 为什么不用库

只有 2 个语种、消息量小。`svelte-i18n` 这类运行时库带的动态加载、日期/数字格式化插件
在这里全用不上——日期数字用原生 `Intl.*` 就够。手写版还能做到：产物里只有字符串本身，
且**缺键直接编译报错**（实测验证：删掉 `en-US.ts` 的一个键，`npm run check` 立刻报
`Property '"error.notFound"' is missing in type ... but required in type 'Record<...>'`）。

消息量涨到几百条、需要按路由拆包时再换 paraglide-js（编译期生成、按消息 tree-shake）。

### 4.2 响应式语言切换

```ts
// 模块级 $state 不能直接 export 可变绑定，所以对外只暴露 getter/setter
let current = $state<Locale>(initialLocale())

export function getLocale(): Locale { return current }
export function setLocale(next: Locale) { ... }

export function t(key: MessageKey, params?): string {
  const template = dicts[current][key] ?? key   // ← 读 $state，模板自动追踪
  ...
}
```

`t()` 内部读的是 `$state`，所以模板里调用 `t('...')` 会建立依赖，`setLocale()` 后自动重渲染，
不需要刷新页面。

### 4.3 语言解析与持久化

```
localStorage['mini-ruoyi.locale'] 存在且在支持列表内 → 用它
否则 navigator.language 以 "zh" 开头              → zh-CN
否则                                              → en-US
```

切换语言时同步写入 localStorage 与 `document.documentElement.lang`。

### 4.4 与后端的键约定

后端只输出 i18n 键（见 [architecture-server.md](architecture-server.md)），
前端字典负责翻译。**新增错误键必须同时改两处**：

- `server/internal/httpx/response.go` 的键常量
- `web/src/lib/i18n/zh-CN.ts`（`en-US.ts` 会因类型不匹配而报错，所以不会漏）

### 4.5 字段级校验错误的渲染

后端只给 `{field, rule, param}`，前端拼文案：

```ts
validationText({ field: 'name', rule: 'min', param: '2' })
// → "名称长度不能少于 2 个字符"
```

- `field` 经 `fieldLabel()` 映射成标签，**未知字段回落到字段名本身**，
  后端加了字段而前端还没补标签时不会显示空白
- `rule` 拼成 `validation.min` 字典键，**未实现的规则回落到 `validation.default`**，不会崩

## 5. 与后端的集成

### 5.1 响应契约

后端统一返回（详见 [architecture.md](architecture.md)）：

```jsonc
{ "code": 0, "msg": "ok", "data": {...} }
{ "code": 1, "msg": "error.validationFailed", "errors": [{ "field", "rule", "param" }] }
```

前端消费规则：

- 成功判定看 **HTTP 状态码**（`res.ok`），`code` 仅作冗余标志
- 失败时 `msg` 是 i18n 键，直接 `t(msg)` 渲染
- 有 `errors[]` 时按字段高亮表单

### 5.2 API 客户端

`src/lib/api/client.ts` 是所有接口调用的唯一入口，职责：

| 职责 | 说明 |
| --- | --- |
| 统一解信封 | 成功返回 `data`，失败抛 `ApiError`（带 `key`、`fieldErrors`、`dependents`） |
| CSRF 注入 | 非 GET 请求自动带 `X-CSRF-Token`，值由会话状态在登录/刷新时设置 |
| 401 处理 | 通过 `setUnauthorizedHandler` 注册的回调清空会话状态并回登录页 |
| 网络异常 | `fetch` 本身失败 → `error.network` |
| 非信封响应 | 判为 `error.backendUnreachable`，见 §5.3 |
| multipart | 单独的 `api.upload()`：**不能设 Content-Type**，boundary 必须由浏览器生成 |

会话状态（`stores/session.svelte.ts`）与客户端之间是**单向依赖**：
client 不认识业务状态，只在 401 时回调。这样 client 可以被独立测试，
也不会因为引入 store 而产生循环依赖。

### 5.3 静态资源加载失败自愈

```ts
// src/main.ts
window.addEventListener('vite:preloadError', () => location.reload())
```

后端直接托管产物，前端重新构建后旧的哈希文件会被清理。已经打开着的标签页若再做懒加载
就会拿到 404，这里直接整页刷新去取最新版本。

### 5.3 ⚠️ 「响应不是信封」意味着请求没到后端

`client.ts` 判断响应的方式不是「HTTP 状态码是否 2xx」，而是**「是不是本服务的信封格式」**
（`{code, msg, ...}`）。因为「后端没起来」和「后端出错了」是两件排查方向完全不同的事：

| 现象 | 判定 | 键 |
| --- | --- | --- |
| `fetch` 抛异常 | 没有任何东西应答（DNS/连接被拒） | `error.network` |
| 有 HTTP 响应，但不是信封 | 中间有代理/网关应答，或后端进程没起 | `error.backendUnreachable` |
| 是信封 | 后端正常处理了，按 `msg` 翻译 | 后端给的键 |

实测：**Vite dev server 在后端没启动时返回 `502 Bad Gateway` + `text/plain` + 空 body**。
早期版本把它归到 `error.internal`，结果是界面提示「服务器内部错误」，
让人去翻后端日志——而后端根本没启动。

这个判断成立的前提是**只要响应来自本服务就一定是信封格式**。为此
`middleware.Recovery()` 替换了 `gin.Recovery()`：后者返回空 body 的 500，
会破坏这个前提，也违反了「所有 API 响应都带信封」的契约。

## 5.4 会话与启动流程

`src/lib/stores/session.svelte.ts` 持有用户、权限码、菜单树与 CSRF 令牌。

```
启动  → bootstrap() 调 /auth/me
        ├ 成功 → authenticated，进入应用外壳
        └ 失败 → anonymous，显示登录页（并把地址栏归到 /login）
```

**刷新页面必须重新问一次 `/auth/me`**：cookie 还在，但 CSRF 令牌只在内存里，
不重新取就没法发任何写请求。

权限判断只有一种写法：`session.can('system:user:add')`。内置管理员的 `perms`
由后端铺满全部权限码，所以前端不需要 `isAdmin ||` 这类分支——少一处漏判的可能。

`client.ts` 收到 401 时会调用 `setUnauthorizedHandler` 注册的回调（即 `clear()`），
从而自动登出。这样 client 保持对业务状态无感知，会话逻辑集中在 store 里。

## 5.5 路由

`src/lib/router.svelte.ts` 约 100 行，没有路由库。

```
菜单树 ──flatten──▶ [{ path, titleKey, component }]
                            │
浏览器路径 ──精确匹配────────┘
                            ▼
              import.meta.glob('../pages/**/*.svelte') 解析 component
```

- 用 `{ eager: true }`，解析是同步的：没有加载态、不会点菜单先闪一下空白。
  页面数量是十几个量级时这个取舍划算；涨到几十个再改成懒加载配 `{#await}`
- `component` 找不到时返回 `null` 并显示一个明确提示，**不抛错**。
  菜单是数据库里的数据，可能手工加过、也可能对应页面已被删除——
  这时应该让人看懂发生了什么，而不是让整个应用崩掉、连导航都点不了
- 登录后若当前路径不在菜单里，会 `replaceTo` 到第一个可访问页面

## 5.6 多标签页

`src/lib/stores/tabs.svelte.ts` 只维护「打开了哪些标签」；**当前激活的是哪一个由路由决定**
（`route.pathname`）。不另存一份 activePath，否则浏览器前进/后退、直接改地址栏都会绕过它。

内容区把**所有标签的页面同时渲染，用 CSS 隐藏非激活的**（见 `app-shell.svelte`）。
这正是多标签页的意义：切回来时表单内容、滚动位置、已加载的数据都还在。
代价是打开的页面越多，常驻的组件实例越多——管理后台的标签数在个位数量级，可以接受。

两个由测试兜住的坑：

1. **只在「路径变化」时开标签，而不是「当前路径是个菜单页」时开。**
   后者会让「关闭全部」失效——关掉之后路径没变，effect 重跑又把标签加回来。
   实现里用普通变量记 `lastOpened`（不是 `$state`，改它不该触发重跑）。
2. **页面不能持续读全局 URL。** 多标签下 URL 只反映激活的那个标签，
   隐藏页面若持续跟随，切标签会冲掉它自己的筛选条件，还会让它拿着别人的参数重新请求。
   所以列表页的分页/筛选是**组件本地状态**，只在挂载时从 URL 读一次
   （刷新仍然有效），写入是单向的。代价是前进/后退不会回填筛选框。
3. **E2E 的选择器必须限定到可见面板。** 所有打开的标签都在 DOM 里（只是隐藏），
   页面内的 `data-testid` 会同时存在多份——`getByTestId('filters')` 会命中多个并触发
   strict mode 报错。`e2e/helpers.ts` 的 `filters()` 已经限定到
   `[data-testid="tab-panel"]:visible`。

## 5.7 ⚠️ 409 是确认信号，不是错误

删除有子数据的资源时，后端返回 `409 + error.hasDependents + 影响面`。
**这不是错误**，前端不能弹错误提示，而要弹确认框。

这条规则封装在 `src/lib/api/delete.ts` 一处：

```ts
const done = await deleteWithConfirm(`/menus/${id}`)
// 内部：DELETE → 若 409 则弹确认框 → 用户确认后 DELETE ...?cascade=true
```

顺序写错的话，用户会先看到红色报错、再看到确认框，完全不知道发生了什么。
所有删除入口都必须走这个封装，不要直接调 `api.del`。

## 6. 样式与组件

### 6.1 Tailwind v4

CSS-first 配置，**没有 `tailwind.config.js`**。主题变量定义在 `src/app.css`：

```css
@import "tailwindcss";
@import "tw-animate-css";
@import "shadcn-svelte/tailwind.css";
@import "@fontsource-variable/inter";

@custom-variant dark (&:is(.dark *));
:root { --background: oklch(...); --primary: oklch(...); ... }
```

修改主题色直接改 `:root` 里的变量。`app.css` 的变量块由 shadcn CLI 生成，
再次运行 `init` 会被覆盖（CLI 会提示确认）。

### 6.2 主题预设

`components.json` 里 `"style": "mira"`，对应 shadcn-svelte 1.7 的 8 个预设之一：
**"Compact. Made for dense interfaces."**——后台管理表格/表单密集，选它。

其余预设：`nova`（紧凑）、`vega`（经典 shadcn 观感）、`maia`（宽松圆润）、
`lyra`（方正、配等宽字体）、`luma`（圆润有质感）、`sera`（编辑风排版）、`rhea`（紧凑版 luma）。

换风格：改 `components.json` 的 `style`，重跑 `npx shadcn-svelte@latest init --reinstall`。

### 6.3 加组件

```bash
cd web
npx shadcn-svelte@latest add dialog select table
```

组件落进 `src/lib/components/ui/<名字>/`，可自由修改。CLI 会自动补必要的 npm 依赖。

### 6.4 用库还是用原生元素

原则：**库只在提供难以复刻的价值时使用，纯样式的一律用平台原生元素。**

实测数据（同一页面的两次构建）：

| 方案 | JS 产物体积 |
| --- | --- |
| 用 shadcn 的 `select` + `checkbox` | 301 kB / **93.1 kB gzip** |
| 换成原生 `<select>` / `<input type="checkbox">` | 215 kB / **71.4 kB gzip** |

两个下拉框和几个勾选框要多付 **约 22 kB gzip**，而原生元素自带键盘导航、
移动端选择与读屏支持。所以本项目：

| 用途 | 选择 | 理由 |
| --- | --- | --- |
| 下拉框、勾选框 | **原生元素** + Tailwind | 库只带来外观，没有不可替代的能力 |
| 对话框 | **shadcn Dialog（bits-ui）** | 焦点陷阱、Esc 关闭、aria、portal 手写容易做错 |
| 表格 | shadcn Table | 纯标记 + 样式，几乎零运行时成本 |

新增组件前先按这个标准过一遍，别默认「有就用」。

### 6.5 表单控件的尺寸约规

所有表单控件走同一个尺寸来源，**不要手写高度**：

| 控件 | 组件 | 高度 |
| --- | --- | --- |
| 文本输入 | `ui/input`（shadcn 生成） | `h-7` |
| 下拉框 | `ui/native-select`（自建） | `h-7` |
| 按钮 | `ui/button` 的 `default` 尺寸 | `h-7` |

`native-select` 是自建组件而不是用 shadcn 的 `Select`：后者基于 bits-ui，
要多付约 20 kB gzip，而原生的键盘导航、移动端选择、读屏支持本来就够用。

**踩过的坑**：曾经各处手写 `<select class="h-9 ...">`，而 `Input` 是 `h-7`，
于是筛选栏里「文本框比下拉框矮一截」。改成共用组件后不可能再出现——
并且 `e2e/form-controls.spec.ts` 会在浏览器里**实测像素高度**，
防止「换了主题预设」或「升级 shadcn 组件」之后这个不一致又回来。

### 6.6 筛选栏的布局

字段用**固定宽度 + 换行**（`flex flex-wrap items-end gap-3`，字段 `w-48`），
不是等分栅格：

- 等分（`grid sm:grid-cols-4`）会让宽屏上的输入框被拉得很长，扫视反而费劲
- 固定宽度下屏幕再宽也只是多放几个，视觉密度稳定
- 按钮（查询/重置）跟在字段后面一起换行，不要推到最右边——固定宽度下会中间空一大片

`e2e/form-controls.spec.ts` 钉住了这两条：字段宽度不随视口变化、窄屏时换行而非压扁。

### 6.7 依赖清单说明

| 包 | 为什么在 |
| --- | --- |
| `bits-ui` | shadcn-svelte 的无头组件底座，当前组件虽未直接引用，但交互类组件（dialog/select/dropdown）都依赖它 |
| `tailwind-variants` | 被 button / badge 引用，用于生成 variant class |
| `cn` | 被 `src/lib/utils.ts` 引用（`export { cn } from "cn"`） |
| `tw-animate-css` | 被 `app.css` `@import` |
| `@lucide/svelte` | `components.json` 声明的图标库 |

`clsx` / `tailwind-merge` 不在直接依赖里——它们由 `cn`、`bits-ui`、`tailwind-variants`
间接引入。若需要直接用，先确认是否真的必要。

## 7. 构建产物与后端集成

```
vite.config.ts
  build.outDir    = 'dist'        # 产物落 web/dist
  build.assetsDir = 'assets'      # 资源落 dist/assets/ ← 必须与后端的 /assets/ 前缀对齐
```

后端按目录约定接管（见 [architecture-server.md](architecture-server.md)）：

```
web/dist/index.html   →  服务根路径，Cache-Control: no-cache
web/dist/assets/*     →  /assets/*，Cache-Control: immutable
```

**当前产物体积**（实测，含 i18n、登录、应用外壳与三个管理页面）：

| 文件 | 原始 | gzip |
| --- | --- | --- |
| `index.html` | 0.5 KB | 0.3 KB |
| `assets/index-*.css` | 42.5 KB | 8.2 KB |
| `assets/index-*.js` | 241 KB | **75.7 KB** |
| `assets/*.woff2`（Inter 可变字体全部子集） | 224 KB | — |
| `dist` 总计 | 400 KB | — |

字体是最大的一块。若只面向中英文，可裁成 latin + latin-ext 子集。

## 8. 开发流程

```bash
make dev-server     # 终端 1：后端 :8080
make dev-web        # 终端 2：Vite :5173，/api 与 /healthz 代理到 :8080
npm run check       # svelte-check + tsc，提交前跑
npm run build       # 生产构建
```

`vite.config.ts` 的 `server.proxy` 只代理 `/api` 和 `/healthz`。新增后端非 `/api` 前缀的
路由时，记得同步加代理规则。

## 9. 待办

| 项 | 说明 |
| --- | --- |
| **多标签页** | 目前内容区只渲染当前页面；标签页需要保留已打开页面的状态 |
| **列表筛选与排序** | 用户/角色列表目前只支持分页，后端也没有筛选参数 |
| **会话管理页** | 后端 `sys_sessions` 表与查询已就位，缺一个「我登录了哪些设备」的列表与踢人入口 |
| **浏览器端验证** | 见下方「验证方式的边界」 |

### 9.1 验证方式

三层，各有分工：

| 层 | 命令 | 覆盖 |
| --- | --- | --- |
| 类型与模板 | `npm run check` | svelte-check + tsc |
| 接口契约 | `make test` | 后端 70+ 用例（CGO 无关的正确性都在这层） |
| 真实交互 | `make test-e2e` | Playwright + Chromium，37 个用例 |

E2E 走 `webServer` 自动起一个**用临时库的后端**，每次运行前清库——
残留数据会让「共 N 条」这类断言变成依赖执行顺序，非常难查。
测试串行执行（`workers: 1`），因为共用一个库。

写 E2E 时的两条经验：

- **通知是叠加的**，`getByText('已保存')` 会命中上一次操作留下的那条，
  让断言在请求失败时也通过。判断「保存成功」要断言**弹窗关闭**。
- **文案相同的按钮会互相干扰**（侧边栏与标签栏、筛选栏与弹窗），
  必须用 `data-testid` 限定作用域，否则触发 Playwright 的 strict mode。

已经由 E2E 抓到过的真实缺陷：新建用户时勾选的角色被静默丢弃
（`if (form.id !== null)` 把新建路径漏掉了），以及「关闭全部」后标签自动回来。
