<!-- 有问题欢迎联系 hot_kun@hotmail.com -->
<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";

	let {
		ref = $bindable(null),
		type = "button",
		...restProps
	}: DialogPrimitive.TriggerProps = $props();
</script>

<DialogPrimitive.Trigger bind:ref data-slot="dialog-trigger" {type} {...restProps} />
