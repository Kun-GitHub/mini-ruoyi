<script lang="ts">
  import type { HTMLSelectAttributes } from 'svelte/elements'
  import type { Snippet } from 'svelte'

  import { cn, type WithElementRef } from '$lib/utils.js'

  type Props = WithElementRef<HTMLSelectAttributes> & {
    class?: string
    children?: Snippet
  }

  // value 必须显式 $bindable：否则调用方的 bind:value 会被 Svelte 拒绝
  let {
    ref = $bindable(null),
    value = $bindable(),
    class: className,
    children,
    ...restProps
  }: Props = $props()
</script>

<!--
  原生下拉框。刻意不用 shadcn 的 Select（bits-ui）：它要付出约 20 kB gzip 的代价，
  而原生的键盘导航、移动端选择、读屏支持本来就够用。

  ⚠️ 样式必须与 ui/input/input.svelte 保持一致——尤其是高度（mira 预设是 h-7）。
  两者不一致的表现是筛选栏里「文本框比下拉框矮一截」，很难看但也不报错。
  e2e/form-controls.spec.ts 有一条用例直接在浏览器里量两者的高度，防止这段漂移。
-->
<select
  bind:this={ref}
  data-slot="native-select"
  class={cn(
    'bg-input/20 dark:bg-input/30 border-input focus-visible:border-ring focus-visible:ring-ring/30 h-7 w-full min-w-0 rounded-md border px-2 py-0.5 text-sm outline-none transition-colors focus-visible:ring-2 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-xs/relaxed',
    className,
  )}
  bind:value
  {...restProps}
>
  {@render children?.()}
</select>
