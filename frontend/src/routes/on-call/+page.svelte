<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { authState } from '$lib/state/auth.svelte';
	import { projectsState } from '$lib/state/projects.svelte';
	import { oncallState } from '$lib/state/oncall.svelte';
	import * as Select from '$lib/components/ui/select';
	import TabsRow from '$lib/components/traceway/tabs-row.svelte';
	import InfoCallout from '$lib/components/traceway/info-callout.svelte';
	import OverviewTab from './overview-tab.svelte';
	import TeamsTab from './teams-tab.svelte';
	import SchedulesTab from './schedules-tab.svelte';
	import PagesTab from './pages-tab.svelte';
	import PoliciesTab from './policies-tab.svelte';

	const TABS = [
		{ value: 'pages', label: 'Pages' },
		{ value: 'overview', label: 'Overview' },
		{ value: 'teams', label: 'Teams' },
		{ value: 'schedules', label: 'Schedules' },
		{ value: 'policies', label: 'Policies' }
	];

	const TAB_DESCRIPTIONS: Record<string, string> = {
		pages:
			'Pages are incidents opened when a rule fires through an escalation policy. Acknowledge a page to stop further escalation, and resolve it once the issue is fixed.',
		overview:
			'A live view of who is on call right now for each team and schedule in the organization, and who takes over next.',
		teams:
			'Teams group organization members and own projects. When a page is opened for a project, it is routed to the team that owns it. Schedules belong to teams.',
		schedules:
			'Schedules define who is on call at any moment using rotation layers and one-off overrides. Exactly one person is on call per schedule at a time.',
		policies:
			'Escalation policies define who gets paged and in what order. If a page is not acknowledged in time, it escalates to the next step. Attach a policy to an alert rule through an Escalation channel.'
	};

	const NEW_BUTTON_LABELS: Record<string, string> = {
		teams: 'New Team',
		schedules: 'New Schedule',
		policies: 'New Policy'
	};

	const orgs = $derived(authState.organizations);

	let selectedOrgId = $state<number | null>(null);

	const initialPageParam = page.url.searchParams.get('page');
	const deepLinkPageId =
		initialPageParam && !Number.isNaN(Number(initialPageParam)) ? Number(initialPageParam) : null;

	// Ack links carry the page's project id: pages resolve against the selected
	// project, so switch before the pages tab issues its first request.
	const initialProjectParam = page.url.searchParams.get('projectId');
	if (initialProjectParam && initialProjectParam !== projectsState.currentProjectId) {
		projectsState.selectProject(initialProjectParam);
	}

	const currentOrganizationId = $derived.by(() => {
		if (selectedOrgId !== null && orgs.some((o) => o.id === selectedOrgId)) {
			return selectedOrgId;
		}
		const projectOrgId = projectsState.currentProject?.organizationId;
		if (projectOrgId && orgs.some((o) => o.id === projectOrgId)) {
			return projectOrgId;
		}
		return orgs[0]?.id ?? null;
	});

	const currentOrganizationName = $derived(
		orgs.find((o) => o.id === currentOrganizationId)?.name ?? ''
	);

	const canManage = $derived(
		currentOrganizationId !== null && oncallState.canManage(currentOrganizationId)
	);

	const activeTab = $derived(page.url.searchParams.get('tab') || 'pages');

	const newButtonLabel = $derived(canManage ? NEW_BUTTON_LABELS[activeTab] : undefined);

	let teamsTab = $state<TeamsTab>();
	let schedulesTab = $state<SchedulesTab>();
	let policiesTab = $state<PoliciesTab>();

	function openNewForTab() {
		if (activeTab === 'teams') teamsTab?.openNew();
		else if (activeTab === 'schedules') schedulesTab?.openNew();
		else if (activeTab === 'policies') policiesTab?.openNew();
	}

	function setTab(tab: string) {
		const url = new URL(window.location.href);
		url.searchParams.set('tab', tab);
		goto(url.toString(), { replaceState: true, noScroll: true });
	}

	$effect(() => {
		if (currentOrganizationId !== null) {
			oncallState.loadTeams(currentOrganizationId);
			oncallState.loadSchedules(currentOrganizationId);
		}
	});

	onMount(() => {
		if (deepLinkPageId !== null) {
			const url = new URL(window.location.href);
			url.searchParams.set('tab', 'pages');
			url.searchParams.delete('page');
			url.searchParams.delete('projectId');
			goto(url.toString(), { replaceState: true, noScroll: true });
		}
	});
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<h1 class="text-3xl font-semibold tracking-tight">On-Call</h1>
		{#if orgs.length > 1}
			<div class="flex items-center gap-2">
				<span class="text-sm text-muted-foreground">Organization</span>
				<Select.Root
					type="single"
					value={currentOrganizationId !== null ? String(currentOrganizationId) : undefined}
					onValueChange={(val) => {
						if (val) selectedOrgId = Number(val);
					}}
				>
					<Select.Trigger class="w-[220px]">
						{currentOrganizationName || 'Select organization'}
					</Select.Trigger>
					<Select.Content>
						{#each orgs as org (org.id)}
							<Select.Item value={String(org.id)}>{org.name}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
		{/if}
	</div>

	<TabsRow
		tabs={TABS}
		{activeTab}
		onTabChange={setTab}
		actionLabel={newButtonLabel}
		onAction={openNewForTab}
	/>

	{#if TAB_DESCRIPTIONS[activeTab]}
		<InfoCallout>{TAB_DESCRIPTIONS[activeTab]}</InfoCallout>
	{/if}

	{#if currentOrganizationId === null}
		<div
			class="flex flex-col items-center justify-center rounded-md bg-muted py-20 text-center text-muted-foreground"
		>
			<p>You are not a member of any organization yet.</p>
		</div>
	{:else if activeTab === 'pages'}
		<PagesTab {deepLinkPageId} />
	{:else if activeTab === 'overview'}
		<OverviewTab organizationId={currentOrganizationId} onGoToTeams={() => setTab('teams')} />
	{:else if activeTab === 'teams'}
		<TeamsTab bind:this={teamsTab} organizationId={currentOrganizationId} {canManage} />
	{:else if activeTab === 'schedules'}
		<SchedulesTab bind:this={schedulesTab} organizationId={currentOrganizationId} {canManage} />
	{:else if activeTab === 'policies'}
		<PoliciesTab bind:this={policiesTab} organizationId={currentOrganizationId} {canManage} />
	{/if}
</div>
