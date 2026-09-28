# mini-ruoyi / web

English | [简体中文](README.md)

The frontend single-page application. Svelte 5 + Vite + TypeScript + Tailwind CSS v4 + shadcn-svelte.

The output is plain static files, hosted from disk by the backend (see [../docs/architecture-web.en.md](../docs/architecture-web.en.md)).
No SvelteKit, no SSR, no Node server runtime.

## Requirements

- Node.js `^20.19.0 || >=22.12.0` (what Vite 8 requires)
- No CLI needs to be installed globally

## Commands

```bash
npm install          # install dependencies
npm run dev          # Vite dev server on :5173
npm run build        # production build into dist/
npm run preview      # preview the build locally
npm run check        # svelte-check + tsc; must pass before committing
npx playwright test  # browser tests (needs make build first)
```

`npx playwright test` needs to reach `bin/mini-ruoyi`, so running `make test-e2e` from the repository root is easier —
it builds first and then starts a backend on a temporary database. The first run needs
`npx playwright install chromium`.

Working from the repository root is easier:

```bash
make deps            # = cd web && npm install
make dev-web         # dev server (/api and /healthz proxied to :8080)
make web             # build into web/dist
```

The output layout of `npm run build` is fixed (configured in `vite.config.ts`) and **must not be changed**:

```
dist/index.html      returned by the backend at the server root
dist/assets/*        returned by the backend under the /assets/ prefix, with a long immutable cache
```

## Talking to the backend

⚠️ **Starting only the frontend does not work**: Vite merely proxies `/api` to `:8080`, so with no backend running the
proxy returns a `502` with an empty body and the UI says "cannot reach the backend service, please make sure it is
running". (That message is deliberate — earlier versions reported `error.internal`, which made people think the backend
had a bug.)

⚠️ **Running `cmd/server` straight from GoLand / VS Code also works**, but know this: the working directory is
`server/` and the database is `server/data.db`. The frontend directory is now auto-detected as `../bin/web` (so run
`make build` once first).

```bash
# terminal 1
make dev-server      # backend on :8080 (falls back to API-only mode when web/ does not exist)

# terminal 2
make dev-web         # Vite on :5173, proxying /api and /healthz to :8080
```

`server.proxy` in `vite.config.ts` proxies only `/api` and `/healthz`.
When you add a backend route with a prefix other than `/api`, remember to add the proxy rule too.

## Directory structure

```
web/
├── components.json             # shadcn-svelte configuration
├── index.html                  # the Vite entry point
├── vite.config.ts              # plugins, the $lib alias, output layout, dev proxy
├── tsconfig.json               # the $lib alias (the shadcn CLI reads this one)
└── src/
    ├── main.ts                 # mount + vite:preloadError handling
    ├── app.css                 # Tailwind entry + shadcn theme variables (CLI-generated)
    ├── App.svelte
    └── lib/
        ├── utils.ts            # cn() (CLI-generated)
        ├── i18n/               # internationalization
        └── components/ui/      # shadcn-svelte components
```

`$lib` points at `src/lib`, and the alias is written in `vite.config.ts` and both tsconfig files; missing any one of
them breaks resolution.

## Internationalization

The dictionaries live in `src/lib/i18n/` and currently support `zh-CN` and `en-US`. It is hand-written, with no runtime
library.

### Adding a string

1. Add the key in `src/lib/i18n/zh-CN.ts` (it is the single source of truth)
2. Add the matching key in `src/lib/i18n/en-US.ts` — **a missing one is a compile error**, not a silent fallback
3. In a component:

```svelte
<script lang="ts">
  import { t } from '$lib/i18n/index.svelte'
</script>

<h1>{t('home.title')}</h1>
<p>{t('validation.min', { field: t('field.name'), param: '2' })}</p>
```

`t()` reads a module-level `$state`, so switching languages re-renders automatically with no page reload.

### Adding a language

1. `src/lib/i18n/xx-XX.ts`, typed as `Record<keyof typeof zhCN, string>`
2. Add an entry to both `locales` and `dicts` in `src/lib/i18n/index.svelte.ts`
3. Done — the language shows up in the switcher automatically

### The convention for backend error keys

The backend returns i18n keys only (such as `error.notFound`), never text. When a new backend error key is added,
`zh-CN.ts` has to be updated in step, or the UI displays the raw key name. The key list is in
[../docs/architecture.en.md](../docs/architecture.en.md).

### Rendering field-level validation errors

The backend returns `{field, rule, param}`, rendered with `validationText()`:

```ts
import { validationText } from '$lib/i18n/errors'

validationText({ field: 'name', rule: 'min', param: '2' })
// → "名称长度不能少于 2 个字符"
```

An unknown field falls back to the field name itself and an unimplemented rule falls back to `validation.default`; neither
throws. Field labels are registered in `fieldLabelKeys` in `src/lib/i18n/errors.ts`.

## Components

The component source lives under `src/lib/components/ui/` and is **source copied into the repository**, not a
dependency, so it can be changed freely.

```bash
npx shadcn-svelte@latest add dialog table select
```

The theme preset is `mira` (compact, suited to dense tables). To change the style, edit `style` in `components.json` and
re-run `npx shadcn-svelte@latest init --reinstall`.

The dependency list and what each entry does are in [../docs/architecture-web.en.md](../docs/architecture-web.en.md).

## Svelte 5 conventions

Use runes, not the old syntax:

| Case | Syntax |
| --- | --- |
| component state | `let count = $state(0)` |
| derived value | `const doubled = $derived(count * 2)` |
| side effect | `$effect(() => { ... })` |
| component props | `let { title } = $props()` |

No state library of the Pinia / Vuex kind is installed (runes are the reactive system), and no axios either (`fetch` is
used).

## Not implemented yet

| Item | Notes |
| --- | --- |
| Tab persistence | Tabs live in memory; after a refresh only the one for the current path is restored |
| List export | Users/roles/logs have no export entry point |
| Shared components | Tables, pagination and filter bars are written once per page for now — good enough, but they could be extracted |

### Built-in routes

The personal profile page (`/profile`) is **not a menu entry**; it is declared in `builtinRoutes` in
`router.svelte.ts`, and its entry point is the username in the top bar. When adding a page, decide first which kind it
is: assignable per role → a menu; something every logged-in account should have → a built-in route.