<script lang="ts">
	import * as Tabs from '$lib/components/ui/tabs';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import { Plus } from '@lucide/svelte';

	interface Tab {
		value: string;
		label: string;
	}

	interface Props {
		tabs: Tab[];
		activeTab: string;
		onTabChange: (tab: string) => void;
		actionLabel?: string;
		onAction?: () => void;
	}

	let { tabs, activeTab, onTabChange, actionLabel, onAction }: Props = $props();

	const activeLabel = $derived(
		tabs.find((t) => t.value === activeTab)?.label ?? tabs[0]?.label ?? ''
	);
</script>

<!-- Container query: the tab list collapses into a full-row dropdown (and the action
     button wraps to its own full row) based on the content width, not the viewport,
     so it accounts for the sidebar. -->
<div class="@container">
	<div class="flex flex-wrap items-center justify-between gap-x-2 gap-y-4">
		<Tabs.Root
			value={activeTab}
			onValueChange={(v) => {
				if (v) onTabChange(v);
			}}
			class="hidden @2xl:block"
		>
			<Tabs.List>
				{#each tabs as tab (tab.value)}
					<Tabs.Trigger value={tab.value}>{tab.label}</Tabs.Trigger>
				{/each}
			</Tabs.List>
		</Tabs.Root>
		<Select.Root
			type="single"
			value={activeTab}
			onValueChange={(v) => {
				if (v) onTabChange(v);
			}}
		>
			<Select.Trigger class="w-full @2xl:hidden">{activeLabel}</Select.Trigger>
			<Select.Content>
				{#each tabs as tab (tab.value)}
					<Select.Item value={tab.value}>{tab.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		{#if actionLabel && onAction}
			<Button size="sm" variant="success" class="w-full @2xl:w-auto" onclick={onAction}>
				<Plus class="mr-1 h-4 w-4" />
				{actionLabel}
			</Button>
		{/if}
	</div>
</div>
