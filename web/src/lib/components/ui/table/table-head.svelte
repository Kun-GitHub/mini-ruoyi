<!-- 有问题欢迎联系 hot_kun@hotmail.com -->
<script lang="ts">
	import { cn, type WithElementRef } from "$lib/utils.js";
	import type { HTMLThAttributes } from "svelte/elements";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLThAttributes> = $props();
</script>

<th bind:this={ref} data-slot="table-head" class={cn("text-foreground h-10 px-2 text-left align-middle text-sm font-medium whitespace-nowrap [&:has([role=checkbox])]:pr-0", className)} {...restProps}>
	{@render children?.()}
</th>
