<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import * as Table from '$lib/components/ui/table';
	import { LoadingCircle } from '$lib/components/ui/loading-circle';
	import EmptyState from '$lib/components/traceway/empty-state.svelte';
	import { oncallState, type Schedule } from '$lib/state/oncall.svelte';
	import ScheduleDialog from './schedule-dialog.svelte';
	import ScheduleDetail from './schedule-detail.svelte';

	interface Props {
		organizationId: number;
		canManage: boolean;
	}

	let { organizationId, canManage }: Props = $props();

	let selectedScheduleId = $state<number | null>(null);
	let scheduleDialogOpen = $state(false);
	let detailRefreshKey = $state(0);

	const schedules = $derived(oncallState.schedules);

	$effect(() => {
		if (selectedScheduleId !== null && !schedules.some((s) => s.id === selectedScheduleId)) {
			selectedScheduleId = null;
		}
	});

	function teamName(teamId: number): string {
		return oncallState.teams.find((t) => t.id === teamId)?.name ?? '—';
	}

	function selectSchedule(schedule: Schedule) {
		selectedScheduleId = selectedScheduleId === schedule.id ? null : schedule.id;
	}

	export function openNew() {
		scheduleDialogOpen = true;
	}
</script>

<div class="space-y-4">
	{#if oncallState.schedulesLoading}
		<div class="flex justify-center py-12"><LoadingCircle size="xlg" /></div>
		{:else if oncallState.schedulesError}
		<div
			class="flex flex-col items-center justify-center gap-3 rounded-md bg-muted py-20 text-center text-muted-foreground"
		>
			<p class="text-sm text-destructive">{oncallState.schedulesError}</p>
			<Button variant="outline" size="sm" onclick={() => oncallState.loadSchedules(organizationId)}>
				Retry
			</Button>
		</div>
{:else if schedules.length === 0}
		<EmptyState
			message="No schedules yet. Create one to get started."
			actionLabel={canManage ? 'Create your first Schedule' : undefined}
			onAction={openNew}
		/>
	{:else}
		<div class="overflow-hidden rounded-md border">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Name</Table.Head>
						<Table.Head>Team</Table.Head>
						<Table.Head>Timezone</Table.Head>
						<Table.Head>Layers</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each schedules as schedule (schedule.id)}
						<Table.Row
							class="cursor-pointer {selectedScheduleId === schedule.id ? 'bg-muted/50' : ''}"
							onclick={() => selectSchedule(schedule)}
						>
							<Table.Cell class="font-medium">{schedule.name}</Table.Cell>
							<Table.Cell>
								<Badge variant="outline">{teamName(schedule.teamId)}</Badge>
							</Table.Cell>
							<Table.Cell class="text-muted-foreground">{schedule.timezone}</Table.Cell>
							<Table.Cell>{schedule.definition?.layers?.length ?? 0}</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
	{/if}

	{#if selectedScheduleId !== null}
		{#key `${selectedScheduleId}-${detailRefreshKey}`}
			<ScheduleDetail
				{organizationId}
				scheduleId={selectedScheduleId}
				{canManage}
				onDeleted={() => {
					selectedScheduleId = null;
				}}
				onChanged={() => {
					detailRefreshKey += 1;
				}}
			/>
		{/key}
	{/if}
</div>

<ScheduleDialog
	bind:open={scheduleDialogOpen}
	{organizationId}
	schedule={null}
	onSaved={(scheduleId) => {
		scheduleDialogOpen = false;
		if (scheduleId) selectedScheduleId = scheduleId;
	}}
/>
