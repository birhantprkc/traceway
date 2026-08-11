<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Plus } from '@lucide/svelte';

	interface Props {
		message: string;
		actionLabel?: string;
		onAction?: () => void;
	}

	let { message, actionLabel, onAction }: Props = $props();
</script>

<div
	class="flex flex-col items-center justify-center rounded-md bg-muted py-20 text-center text-muted-foreground"
>
	<p class={actionLabel && onAction ? 'mb-4' : ''}>{message}</p>
	{#if actionLabel && onAction}
		<Button variant="success" onclick={onAction}>
			<Plus class="mr-1 h-4 w-4" />
			{actionLabel}
		</Button>
	{/if}
</div>
