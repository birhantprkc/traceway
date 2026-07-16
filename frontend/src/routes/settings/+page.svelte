<script lang="ts">
    import { goto } from '$app/navigation';
    import { authState } from '$lib/state/auth.svelte';
    import { projectsState } from '$lib/state/projects.svelte';
    import { organizationState } from '$lib/state/organization.svelte';
    import * as Select from '$lib/components/ui/select';
    import OrganizationTab from './organization-tab.svelte';
    import UsersTab from './users-tab.svelte';
    import type { Component } from 'svelte';
    import { LoadingCircle } from '$lib/components/ui/loading-circle';

    let loading = $state(true);
    let error = $state<string | null>(null);
    let BillingTab = $state<Component<{ organizationId: number }> | null>(null);

    async function loadBillingModule() {
        try {
            // @ts-ignore - $billing alias only exists when billing extension is available
            const module = await import('$billing/billing-tab.svelte');
            BillingTab = module.default;
        } catch {
            // Billing extension not available - this is expected for open source builds
        }
    }

    $effect(() => {
        loadBillingModule();
    });

    const manageableOrgs = $derived(
        authState.organizations.filter(o => o.role === 'owner' || o.role === 'admin')
    );

    let selectedOrgId = $state<number | null>(null);

    const currentOrganizationId = $derived.by(() => {
        if (selectedOrgId !== null && manageableOrgs.some(o => o.id === selectedOrgId)) {
            return selectedOrgId;
        }
        const projectOrgId = projectsState.currentProject?.organizationId;
        if (projectOrgId && manageableOrgs.some(o => o.id === projectOrgId)) {
            return projectOrgId;
        }
        return manageableOrgs[0]?.id ?? null;
    });

    const currentOrganizationName = $derived(
        manageableOrgs.find(o => o.id === currentOrganizationId)?.name ?? ''
    );

    const hasAccess = $derived(currentOrganizationId !== null);

    $effect(() => {
        if (!hasAccess) {
            goto('/');
        }
    });

    $effect(() => {
        if (currentOrganizationId && hasAccess) {
            loading = true;
            organizationState.loadSettings(currentOrganizationId)
                .catch(e => {
                    error = e instanceof Error ? e.message : 'Failed to load settings';
                })
                .finally(() => {
                    loading = false;
                });
        }
    });
</script>

<div class="space-y-4">
    <div class="flex items-center justify-between gap-4">
        <h1 class="text-3xl font-semibold tracking-tight">Settings</h1>
        {#if manageableOrgs.length > 1}
            <div class="flex items-center gap-2">
                <span class="text-sm text-muted-foreground">Organization</span>
                <Select.Root
                    type="single"
                    value={currentOrganizationId !== null ? String(currentOrganizationId) : undefined}
                    onValueChange={(val) => { if (val) selectedOrgId = Number(val); }}
                >
                    <Select.Trigger class="w-[220px]">
                        {currentOrganizationName || 'Select organization'}
                    </Select.Trigger>
                    <Select.Content>
                        {#each manageableOrgs as org (org.id)}
                            <Select.Item value={String(org.id)}>{org.name}</Select.Item>
                        {/each}
                    </Select.Content>
                </Select.Root>
            </div>
        {/if}
    </div>

    {#if loading}
        <div class="flex items-center justify-center py-12">
            <LoadingCircle size="xlg" />
        </div>
    {:else if error}
        <div class="text-center py-12 text-destructive">
            {error}
        </div>
    {:else}
        <div class="space-y-4">
            <OrganizationTab />
            <UsersTab organizationId={currentOrganizationId!} />
            {#if BillingTab}
                <BillingTab organizationId={currentOrganizationId!} />
            {/if}
        </div>
    {/if}
</div>
