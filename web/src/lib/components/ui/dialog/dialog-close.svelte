<!-- 有问题欢迎联系 hot_kun@hotmail.com -->
<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";

	let {
		ref = $bindable(null),
		type = "button",
		...restProps
	}: DialogPrimitive.CloseProps = $props();
</script>

<DialogPrimitive.Close bind:ref data-slot="dialog-close" {type} {...restProps} />
