<!-- 有问题欢迎联系 hot_kun@hotmail.com -->
<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";

	let { ...restProps }: DialogPrimitive.PortalProps = $props();
</script>

<DialogPrimitive.Portal {...restProps} />
