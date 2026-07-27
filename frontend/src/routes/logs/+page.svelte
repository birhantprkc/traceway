<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { browser } from '$app/environment';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import * as Table from '$lib/components/ui/table';
	import { SearchBar } from '$lib/components/ui/search-bar';
	import { LoadingCircle } from '$lib/components/ui/loading-circle';
	import { projectsState } from '$lib/state/projects.svelte';
	import { getTimezone } from '$lib/state/timezone.svelte';
	import { TracewayTableHeader } from '$lib/components/ui/traceway-table-header';
	import { TableEmptyState } from '$lib/components/ui/table-empty-state';
	import { TimeRangePicker } from '$lib/components/ui/time-range-picker';
	import { SeverityBadge } from '$lib/components/ui/severity-badge';
	import * as Select from '$lib/components/ui/select';
	import { Input } from '$lib/components/ui/input';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import ExpandedLogRow from '$lib/components/trace-logs/expanded-log-row.svelte';
	import LogMessage from '$lib/components/trace-logs/log-message.svelte';
	import { Check, Play, Plus, X } from '@lucide/svelte';
	import { CalendarDate } from '@internationalized/date';
	import {
		parseTimeRangeFromUrl,
		getResolvedTimeRange,
		getTimeRangeFromPreset,
		dateToCalendarDate,
		dateToTimeString
	} from '$lib/utils/url-params';
	import { calendarDateTimeToLuxon, toUTCISO, formatDateTime } from '$lib/utils/formatters';
	import {
		getSortState,
		setSortState,
		handleSortClick,
		type SortDirection
	} from '$lib/utils/sort-storage';

	const timezone = $derived(getTimezone());

	const SORT_STORAGE_KEY = 'logs';
	const initialSort = getSortState(SORT_STORAGE_KEY, { field: 'timestamp', direction: 'desc' });
	let sortField = $state(initialSort.field);
	let sortDirection = $state<SortDirection>(initialSort.direction);

	type LogRecord = {
		id: string;
		projectId: string;
		timestamp: string;
		traceId: string;
		spanId: string;
		traceFlags: number;
		severityText: string;
		severityNumber: number;
		serviceName: string;
		body: string;
		resourceSchemaUrl: string;
		resourceAttributes: Record<string, string> | null;
		scopeSchemaUrl: string;
		scopeName: string;
		scopeVersion: string;
		scopeAttributes: Record<string, string> | null;
		logAttributes: Record<string, string> | null;
	};

	let logs = $state<LogRecord[]>([]);
	let loading = $state(true);
	let error = $state('');
	let expandedId = $state<string | null>(null);

	const pageSize = 50;
	let total = $state(0);
	let loadedPages = 1;
	let loadingMore = $state(false);
	let loadMoreError = $state('');

	type LogsResponse = { data: LogRecord[]; pagination: { total: number; totalPages: number } };

	// Live tail mode: poll only the diff since the last poll and prepend it.
	const LIVE_POLL_MS = 5000;
	// Re-query a little of the already-covered window each poll so records that
	// arrive late (SDK batching) aren't missed; dedup by id drops the overlap.
	const LIVE_OVERLAP_MS = 10_000;
	// Cap the buffer so a long-running live session can't grow the DOM forever.
	const MAX_ROWS = 2000;
	let live = $state(false);
	let liveTimer: ReturnType<typeof setInterval> | null = null;
	let pollBusy = false;
	let liveSinceMs = 0;
	// Time range used by the last full load. loadMore and livePoll reuse it so
	// page offsets stay stable while the wall clock moves.
	let lastFromISO = '';
	let lastToISO = '';

	// Windowed rendering: only rows near the viewport are mounted, spacer rows
	// keep the scrollbar honest. Row height is measured from the first rendered
	// row; the (single) expanded row's extra height is measured separately.
	const OVERSCAN = 10;
	let scrollEl = $state<HTMLDivElement | null>(null);
	let scrollTop = $state(0);
	let viewportH = $state(600);
	let rowH = $state(44);
	let expandedHeight = $state(0);

	const expandedIndex = $derived(expandedId ? logs.findIndex((l) => l.id === expandedId) : -1);

	function offsetOf(i: number): number {
		return i * rowH + (expandedIndex >= 0 && i > expandedIndex ? expandedHeight : 0);
	}

	function indexAt(y: number): number {
		if (expandedIndex < 0) return Math.floor(y / rowH);
		const afterExpandedTop = (expandedIndex + 1) * rowH + expandedHeight;
		if (y < (expandedIndex + 1) * rowH) return Math.floor(y / rowH);
		if (y < afterExpandedTop) return expandedIndex;
		return expandedIndex + 1 + Math.floor((y - afterExpandedTop) / rowH);
	}

	const winStart = $derived(Math.max(0, indexAt(scrollTop) - OVERSCAN));
	const winEnd = $derived(Math.min(logs.length, indexAt(scrollTop + viewportH) + 1 + OVERSCAN));
	const topPad = $derived(offsetOf(winStart));
	const bottomPad = $derived(Math.max(0, offsetOf(logs.length) - offsetOf(winEnd)));
	const visibleLogs = $derived(logs.slice(winStart, winEnd));

	// Size the scroll container so the table ends at the bottom of the viewport
	// instead of overflowing the page. max-height (not height) so a short result
	// list doesn't leave a stretched empty table.
	function updateMaxHeight() {
		if (!scrollEl) return;
		const top = scrollEl.getBoundingClientRect().top;
		scrollEl.style.maxHeight = `${Math.max(240, window.innerHeight - top - 24)}px`;
	}

	$effect(() => {
		// Anything that can move the table's top edge (wrapping filter pills,
		// loading states) should trigger a re-measure.
		void metaFilterChips.length;
		void attributeFilters.length;
		void loading;
		updateMaxHeight();
	});

	$effect(() => {
		void logs.length;
		void expandedId;
		if (!scrollEl) return;
		const first = scrollEl.querySelector('tr[data-log-id]') as HTMLElement | null;
		if (first && first.offsetHeight > 0) rowH = first.offsetHeight;
		if (expandedId) {
			const main = scrollEl.querySelector(`tr[data-log-id="${CSS.escape(expandedId)}"]`);
			const expanded = main?.nextElementSibling as HTMLElement | null;
			expandedHeight = expanded?.offsetHeight ?? 0;
		} else {
			expandedHeight = 0;
		}
	});

	type AttributeFilter = {
		scope: 'resource' | 'scope' | 'log';
		key: string;
		value: string;
		exclude: boolean;
	};

	// Parse `resource.service.name=backend-service` or `log.method!=Enter`
	// (scope.*/log.* likewise) into a structured filter. Returns null if the
	// input doesn't match the shape.
	function parseAttributeFilter(input: string): AttributeFilter | null {
		const trimmed = input.trim();
		const m = trimmed.match(/^(resource|scope|log)\.([^=]+?)(!=|=)(.*)$/);
		if (!m) return null;
		const key = m[2].trim();
		if (!key) return null;
		return {
			scope: m[1] as AttributeFilter['scope'],
			key,
			value: m[4],
			exclude: m[3] === '!='
		};
	}

	function formatAttributeFilter(f: AttributeFilter): string {
		return `${f.scope}.${f.key}${f.exclude ? '!=' : '='}${f.value}`;
	}

	function parseLogsUrlParams() {
		if (!browser) {
			return {
				preset: '24h',
				from: null,
				to: null,
				search: '',
				searchType: 'body',
				minSeverity: 0,
				serviceName: '',
				traceId: '',
				spanId: '',
				scopeName: '',
				body: '',
				attributeFilters: [] as AttributeFilter[]
			};
		}
		const params = new URLSearchParams(window.location.search);
		const timeParams = parseTimeRangeFromUrl(timezone, '24h');
		const minSev = Number(params.get('minSeverity') || '0');
		const attrs: AttributeFilter[] = [];
		for (const raw of params.getAll('attr')) {
			const parsed = parseAttributeFilter(raw);
			if (parsed) attrs.push(parsed);
		}
		return {
			...timeParams,
			search: params.get('search') || '',
			searchType: params.get('searchType') || 'body',
			minSeverity: Number.isFinite(minSev) ? minSev : 0,
			serviceName: params.get('service') || '',
			traceId: params.get('traceId') || '',
			spanId: params.get('spanId') || '',
			scopeName: params.get('scopeName') || '',
			body: params.get('body') || '',
			attributeFilters: attrs
		};
	}

	const initialUrlParams = parseLogsUrlParams();
	const initialRange = getResolvedTimeRange(initialUrlParams, timezone);

	let selectedPreset = $state<string | null>(initialUrlParams.preset);
	let fromDate = $state<CalendarDate>(dateToCalendarDate(initialRange.from, timezone));
	let toDate = $state<CalendarDate>(dateToCalendarDate(initialRange.to, timezone));
	let fromTime = $state(dateToTimeString(initialRange.from, timezone));
	let toTime = $state(dateToTimeString(initialRange.to, timezone));

	let searchQuery = $state(initialUrlParams.search);
	let searchType = $state(initialUrlParams.searchType);
	let minSeverity = $state<number>(initialUrlParams.minSeverity);
	let serviceName = $state(initialUrlParams.serviceName);
	let traceIdFilter = $state(initialUrlParams.traceId);
	let spanIdFilter = $state(initialUrlParams.spanId);
	let scopeNameFilter = $state(initialUrlParams.scopeName);
	let bodyFilter = $state(initialUrlParams.body);
	let attributeFilters = $state<AttributeFilter[]>(initialUrlParams.attributeFilters);

	// Add-filter dialog
	let addFilterOpen = $state(false);
	let dialogKey = $state('');
	let dialogValue = $state('');
	let dialogExclude = $state(false);
	let dialogError = $state('');
	// Index into attributeFilters when editing an existing pill, null when adding.
	let dialogEditIndex = $state<number | null>(null);

	function openAddFilterDialog() {
		dialogKey = '';
		dialogValue = '';
		dialogExclude = false;
		dialogError = '';
		dialogEditIndex = null;
		addFilterOpen = true;
	}

	function openEditFilterDialog(index: number) {
		const f = attributeFilters[index];
		dialogKey = `${f.scope}.${f.key}`;
		dialogValue = f.value;
		dialogExclude = f.exclude;
		dialogError = '';
		dialogEditIndex = index;
		addFilterOpen = true;
	}

	function submitDialogFilter() {
		const composed = `${dialogKey.trim()}${dialogExclude ? '!=' : '='}${dialogValue}`;
		const parsed = parseAttributeFilter(composed);
		if (!parsed) {
			dialogError = 'Key must start with resource., scope., or log.';
			return;
		}
		if (dialogEditIndex !== null) {
			updateAttributeFilter(dialogEditIndex, parsed);
		} else {
			addAttributeFilter(parsed);
		}
		addFilterOpen = false;
	}

	function updateAttributeFilter(index: number, filter: AttributeFilter) {
		const dup = attributeFilters.some(
			(f, i) =>
				i !== index &&
				f.scope === filter.scope &&
				f.key === filter.key &&
				f.value === filter.value &&
				f.exclude === filter.exclude
		);
		attributeFilters = dup
			? attributeFilters.filter((_, i) => i !== index)
			: attributeFilters.map((f, i) => (i === index ? filter : f));
		loadData(true);
	}

	function addAttributeFilter(filter: AttributeFilter) {
		const dup = attributeFilters.some(
			(f) =>
				f.scope === filter.scope &&
				f.key === filter.key &&
				f.value === filter.value &&
				f.exclude === filter.exclude
		);
		if (!dup) {
			attributeFilters = [...attributeFilters, filter];
		}
		loadData(true);
	}

	// Displayed attribute keys are prefixed `resource.` / `scope.`; log
	// attributes are unprefixed.
	function displayKeyToFilter(displayKey: string): { scope: AttributeFilter['scope']; key: string } {
		if (displayKey.startsWith('resource.')) {
			return { scope: 'resource', key: displayKey.slice('resource.'.length) };
		}
		if (displayKey.startsWith('scope.')) {
			return { scope: 'scope', key: displayKey.slice('scope.'.length) };
		}
		return { scope: 'log', key: displayKey };
	}

	function attributeFilterState(
		displayKey: string,
		value: string
	): 'none' | 'include' | 'exclude' {
		const { scope, key } = displayKeyToFilter(displayKey);
		const f = attributeFilters.find(
			(f) => f.scope === scope && f.key === key && f.value === value
		);
		if (!f) return 'none';
		return f.exclude ? 'exclude' : 'include';
	}

	// Toggle from an attribute tile in the expanded log row: on/off. An active
	// filter of either kind (include, or exclude via a hand-edited URL) clears.
	function toggleAttributeFilter(displayKey: string, value: string) {
		const { scope, key } = displayKeyToFilter(displayKey);
		const existing = attributeFilters.findIndex(
			(f) => f.scope === scope && f.key === key && f.value === value
		);
		if (existing === -1) {
			attributeFilters = [...attributeFilters, { scope, key, value, exclude: false }];
		} else {
			attributeFilters = attributeFilters.filter((_, i) => i !== existing);
		}
		loadData(true);
	}

	// Filters for the fixed tiles of the expanded row (Body / Trace ID /
	// Span ID / Scope), backed by their dedicated query params rather than
	// attribute filters.
	type MetaFilterField = 'body' | 'traceId' | 'spanId' | 'scopeName';

	function metaFilterState(field: MetaFilterField, value: string): 'none' | 'include' {
		switch (field) {
			case 'body':
				return bodyFilter === value ? 'include' : 'none';
			case 'traceId':
				return traceIdFilter === value ? 'include' : 'none';
			case 'spanId':
				return spanIdFilter === value ? 'include' : 'none';
			case 'scopeName':
				return scopeNameFilter === value ? 'include' : 'none';
		}
	}

	function toggleMetaFilter(field: MetaFilterField, value: string) {
		const active = metaFilterState(field, value) === 'include';
		switch (field) {
			case 'body':
				bodyFilter = active ? '' : value;
				break;
			case 'traceId':
				traceIdFilter = active ? '' : value;
				break;
			case 'spanId':
				spanIdFilter = active ? '' : value;
				break;
			case 'scopeName':
				scopeNameFilter = active ? '' : value;
				break;
		}
		loadData(true);
	}

	// Pills for the active meta filters, shown next to the attribute chips.
	const metaFilterChips = $derived([
		...(bodyFilter ? [{ field: 'body' as const, label: 'body', value: bodyFilter }] : []),
		...(traceIdFilter ? [{ field: 'traceId' as const, label: 'trace_id', value: traceIdFilter }] : []),
		...(spanIdFilter ? [{ field: 'spanId' as const, label: 'span_id', value: spanIdFilter }] : []),
		...(scopeNameFilter
			? [{ field: 'scopeName' as const, label: 'scope', value: scopeNameFilter }]
			: [])
	]);

	function shortenChipValue(value: string): string {
		return value.length > 20 ? value.slice(0, 18) + '…' : value;
	}

	function handleDialogKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			submitDialogFilter();
		}
	}

	const searchTypeOptions = [
		{ value: 'body', label: 'Message' },
		{ value: 'service', label: 'Service' },
		{ value: 'trace', label: 'Trace ID' }
	];

	const severityOptions = [
		{ value: '0', label: 'All levels' },
		{ value: '1', label: 'TRACE+' },
		{ value: '5', label: 'DEBUG+' },
		{ value: '9', label: 'INFO+' },
		{ value: '13', label: 'WARN+' },
		{ value: '17', label: 'ERROR+' },
		{ value: '21', label: 'FATAL' }
	];

	const severityTriggerLabel = $derived(
		severityOptions.find((o) => Number(o.value) === minSeverity)?.label ?? 'All levels'
	);

	function getFromDateTimeUTC(): string {
		const [hour, minute] = fromTime.split(':').map(Number);
		const luxonDt = calendarDateTimeToLuxon(
			{ year: fromDate.year, month: fromDate.month, day: fromDate.day, hour, minute },
			timezone
		);
		return toUTCISO(luxonDt);
	}

	function getToDateTimeUTC(): string {
		const [hour, minute] = toTime.split(':').map(Number);
		const luxonDt = calendarDateTimeToLuxon(
			{ year: toDate.year, month: toDate.month, day: toDate.day, hour, minute },
			timezone
		).endOf('minute');
		return toUTCISO(luxonDt);
	}

	function updateLogsUrl(pushToHistory = true) {
		if (!browser) return;
		const urlParams = new URLSearchParams();
		if (selectedPreset) {
			urlParams.set('preset', selectedPreset);
		} else {
			urlParams.set('from', getFromDateTimeUTC());
			urlParams.set('to', getToDateTimeUTC());
		}
		if (searchQuery.trim()) urlParams.set('search', searchQuery.trim());
		if (searchType !== 'body') urlParams.set('searchType', searchType);
		if (minSeverity > 0) urlParams.set('minSeverity', String(minSeverity));
		if (serviceName.trim()) urlParams.set('service', serviceName.trim());
		if (traceIdFilter.trim()) urlParams.set('traceId', traceIdFilter.trim());
		if (spanIdFilter.trim()) urlParams.set('spanId', spanIdFilter.trim());
		if (scopeNameFilter.trim()) urlParams.set('scopeName', scopeNameFilter.trim());
		if (bodyFilter) urlParams.set('body', bodyFilter);
		for (const f of attributeFilters) {
			urlParams.append('attr', formatAttributeFilter(f));
		}
		const newUrl = `${window.location.pathname}?${urlParams.toString()}`;
		// eslint-disable-next-line svelte/no-navigation-without-resolve
		goto(newUrl, { replaceState: !pushToHistory, noScroll: true, keepFocus: true });
	}

	function removeAttributeFilter(index: number) {
		attributeFilters = attributeFilters.filter((_, i) => i !== index);
		loadData(true);
	}

	function currentFilterBody() {
		return {
			search: searchQuery.trim(),
			searchType,
			minSeverity,
			serviceName: serviceName.trim(),
			traceId: traceIdFilter.trim(),
			spanId: spanIdFilter.trim(),
			scopeName: scopeNameFilter.trim(),
			body: bodyFilter,
			attributeFilters
		};
	}

	async function loadData(pushToHistory = true) {
		loading = true;
		error = '';
		loadMoreError = '';
		loadedPages = 1;

		if (selectedPreset) {
			const range = getTimeRangeFromPreset(selectedPreset, timezone);
			fromDate = dateToCalendarDate(range.from, timezone);
			toDate = dateToCalendarDate(range.to, timezone);
			fromTime = dateToTimeString(range.from, timezone);
			toTime = dateToTimeString(range.to, timezone);
		}

		updateLogsUrl(pushToHistory);

		try {
			lastFromISO = getFromDateTimeUTC();
			lastToISO = getToDateTimeUTC();
			const requestBody = {
				fromDate: lastFromISO,
				toDate: lastToISO,
				orderBy: sortField,
				sortDirection,
				pagination: { page: 1, pageSize },
				...currentFilterBody()
			};

			const response = (await api.post('/logs', requestBody, {
				projectId: projectsState.currentProjectId ?? undefined
			})) as LogsResponse;

			logs = response.data || [];
			total = response.pagination.total;
			scrollEl?.scrollTo({ top: 0 });
			scrollTop = 0;
		} catch (e: any) {
			console.error(e);
			error = e.message || 'Failed to load logs';
		} finally {
			loading = false;
		}
	}

	async function loadMore() {
		if (loadingMore || loading) return;
		loadingMore = true;
		loadMoreError = '';
		try {
			const requestBody = {
				fromDate: lastFromISO,
				toDate: lastToISO,
				orderBy: sortField,
				sortDirection,
				pagination: { page: loadedPages + 1, pageSize },
				...currentFilterBody()
			};

			const response = (await api.post('/logs', requestBody, {
				projectId: projectsState.currentProjectId ?? undefined
			})) as LogsResponse;

			// Live-prepended rows shift page offsets, so drop anything already loaded.
			const seen = new Set(logs.map((l) => l.id));
			const fresh = (response.data || []).filter((l) => !seen.has(l.id));
			logs = [...logs, ...fresh];
			loadedPages += 1;
			total = response.pagination.total;
		} catch (e: any) {
			console.error(e);
			loadMoreError = e.message || 'Failed to load more logs';
		} finally {
			loadingMore = false;
		}
	}

	async function livePoll() {
		if (pollBusy || loading) return;
		pollBusy = true;
		try {
			const nowMs = Date.now();
			const response = (await api.post(
				'/logs',
				{
					fromDate: new Date(liveSinceMs - LIVE_OVERLAP_MS).toISOString(),
					toDate: new Date(nowMs).toISOString(),
					orderBy: 'timestamp',
					sortDirection: 'desc',
					pagination: { page: 1, pageSize },
					...currentFilterBody()
				},
				{ projectId: projectsState.currentProjectId ?? undefined }
			)) as LogsResponse;

			const seen = new Set(logs.map((l) => l.id));
			const fresh = (response.data || []).filter((l) => !seen.has(l.id));
			if (fresh.length > 0) {
				logs = [...fresh, ...logs].slice(0, MAX_ROWS);
				total += fresh.length;
			}
			liveSinceMs = nowMs;
		} catch (e) {
			console.error(e);
		} finally {
			pollBusy = false;
		}
	}

	function toggleLive() {
		if (live) {
			stopLive();
			return;
		}
		// Live tail only makes sense newest-first: clear any custom sort.
		if (sortField !== 'timestamp' || sortDirection !== 'desc') {
			sortField = 'timestamp';
			sortDirection = 'desc';
			setSortState(SORT_STORAGE_KEY, { field: sortField, direction: sortDirection });
		}
		live = true;
		liveSinceMs = Date.now();
		loadData(true);
		liveTimer = setInterval(livePoll, LIVE_POLL_MS);
	}

	function stopLive() {
		live = false;
		if (liveTimer) {
			clearInterval(liveTimer);
			liveTimer = null;
		}
	}

	function handleTimeRangeChange(
		from: { date: CalendarDate; time: string },
		to: { date: CalendarDate; time: string },
		preset: string | null
	) {
		fromDate = from.date;
		toDate = to.date;
		fromTime = from.time;
		toTime = to.time;
		selectedPreset = preset;
		// A custom range is a fixed window in the past; there is nothing to tail.
		if (!preset && live) stopLive();
		loadData(true);
	}

	function handleSearch() {
		loadData(true);
	}

	function handleSeverityChange(value: string) {
		// Keep severity as pending query state - applied on the next Go press,
		// same as the search input. Avoids triggering a re-fetch on every
		// dropdown change.
		minSeverity = Number(value) || 0;
	}

	function handleSort(field: string) {
		// Live tail is pinned to newest-first; sorting drops back to history mode.
		if (live) stopLive();
		const newSort = handleSortClick(field, sortField, sortDirection);
		sortField = newSort.field;
		sortDirection = newSort.direction;
		setSortState(SORT_STORAGE_KEY, newSort);
		loadData(true);
	}

	function handlePopState() {
		const urlParams = parseLogsUrlParams();
		const range = getResolvedTimeRange(urlParams, timezone);
		selectedPreset = urlParams.preset;
		fromDate = dateToCalendarDate(range.from, timezone);
		toDate = dateToCalendarDate(range.to, timezone);
		fromTime = dateToTimeString(range.from, timezone);
		toTime = dateToTimeString(range.to, timezone);
		searchQuery = urlParams.search;
		searchType = urlParams.searchType;
		minSeverity = urlParams.minSeverity;
		serviceName = urlParams.serviceName;
		traceIdFilter = urlParams.traceId;
		spanIdFilter = urlParams.spanId;
		scopeNameFilter = urlParams.scopeName;
		bodyFilter = urlParams.body;
		attributeFilters = urlParams.attributeFilters;
		if (!urlParams.preset && live) stopLive();
		loadData(false);
	}

	function toggleExpanded(id: string) {
		expandedId = expandedId === id ? null : id;
	}

	onMount(() => {
		window.addEventListener('popstate', handlePopState);
		window.addEventListener('resize', updateMaxHeight);
		loadData(false);
	});

	onDestroy(() => {
		stopLive();
		if (typeof window !== 'undefined') {
			window.removeEventListener('popstate', handlePopState);
			window.removeEventListener('resize', updateMaxHeight);
		}
	});
</script>

<div class="space-y-4">
	<div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
		<h2 class="text-3xl font-semibold tracking-tight">Logs</h2>
		<div class="flex w-full items-center gap-2 sm:w-auto">
			{#if selectedPreset === null}
				<Tooltip.Root>
					<!-- buttonVariants sets aria-disabled:pointer-events-none, which would
					     swallow hover and prevent this tooltip from ever opening. -->
					<Tooltip.Trigger
						class="{buttonVariants({
							variant: 'outline'
						})} shrink-0 !pointer-events-auto !cursor-not-allowed opacity-50"
						aria-disabled="true"
					>
						<Play class="h-4 w-4" />
						Live
					</Tooltip.Trigger>
					<Tooltip.Content>
						Live follows the current time, so it's unavailable with a custom time range. Pick a
						preset like "24 hours" to tail logs.
					</Tooltip.Content>
				</Tooltip.Root>
			{:else}
				<Button
					variant={live ? 'default' : 'outline'}
					class="shrink-0"
					aria-pressed={live}
					title={live ? 'Stop live tail' : 'Tail new logs in realtime'}
					onclick={toggleLive}
				>
					<Play class="h-4 w-4 {live ? 'fill-current' : ''}" />
					Live
				</Button>
			{/if}
			<TimeRangePicker
				bind:fromDate
				bind:toDate
				bind:fromTime
				bind:toTime
				bind:preset={selectedPreset}
				onApply={handleTimeRangeChange}
			/>
		</div>
	</div>

	<div class="flex flex-col gap-3 sm:flex-row sm:items-start">
		<div class="flex-1">
			<SearchBar
				placeholder="Search logs..."
				bind:value={searchQuery}
				bind:typeValue={searchType}
				typeOptions={searchTypeOptions}
				onSearch={handleSearch}
				disabled={loading}
			>
				<Select.Root
					type="single"
					value={String(minSeverity)}
					onValueChange={handleSeverityChange}
				>
					<Select.Trigger class="h-9 w-[130px] rounded-none border-r-0 shadow-none">
						{severityTriggerLabel}
					</Select.Trigger>
					<Select.Content>
						{#each severityOptions as opt (opt.value)}
							<Select.Item value={opt.value} label={opt.label}>{opt.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</SearchBar>
		</div>
	</div>

	<div class="flex flex-wrap items-center gap-2">
		<button
			type="button"
			class="inline-flex items-center gap-1 rounded-full border border-dashed px-3 py-0.5 text-xs font-medium text-muted-foreground transition-colors hover:border-foreground/40 hover:text-foreground"
			onclick={openAddFilterDialog}
			disabled={loading}
		>
			<Plus class="h-3 w-3" />
			Add filter
		</button>
		{#each metaFilterChips as chip (chip.field)}
			<span class="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs font-mono">
				<Tooltip.Root>
					<Tooltip.Trigger class="inline-flex cursor-default items-center gap-1">
						<span class="text-muted-foreground">{chip.label}</span>
						<span>=</span>
						<span>{shortenChipValue(chip.value)}</span>
					</Tooltip.Trigger>
					<Tooltip.Content>Not editable</Tooltip.Content>
				</Tooltip.Root>
				<button
					type="button"
					aria-label="Remove filter"
					class="ml-1 cursor-pointer text-muted-foreground hover:text-foreground"
					onclick={() => toggleMetaFilter(chip.field, chip.value)}
				>
					<X class="h-3 w-3" />
				</button>
			</span>
		{/each}
		{#each attributeFilters as f, i (i)}
			<span
				class="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs font-mono"
			>
				<button
					type="button"
					aria-label="Edit filter"
					class="inline-flex cursor-pointer items-center gap-1 hover:text-primary"
					onclick={() => openEditFilterDialog(i)}
				>
					<span class="text-muted-foreground">{f.scope}.{f.key}</span>
					<span class={f.exclude ? 'text-red-500' : ''}>{f.exclude ? '!=' : '='}</span>
					<span>{f.value}</span>
				</button>
				<button
					type="button"
					aria-label="Remove filter"
					class="ml-1 cursor-pointer text-muted-foreground hover:text-foreground"
					onclick={() => removeAttributeFilter(i)}
				>
					<X class="h-3 w-3" />
				</button>
			</span>
		{/each}
	</div>

	<AlertDialog.Root open={addFilterOpen} onOpenChange={(open) => (addFilterOpen = open)}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title>
					{dialogEditIndex !== null ? 'Edit attribute filter' : 'Add attribute filter'}
				</AlertDialog.Title>
				<AlertDialog.Description>
					Attribute keys must start with <code class="font-mono">resource.</code>,
					<code class="font-mono">scope.</code>, or <code class="font-mono">log.</code>.
				</AlertDialog.Description>
			</AlertDialog.Header>
			<div class="flex flex-col gap-3">
				<label class="flex flex-col gap-1 text-sm">
					<span class="font-medium">Attribute key</span>
					<Input
						placeholder="resource.service.name"
						bind:value={dialogKey}
						onkeydown={handleDialogKeydown}
					/>
				</label>
				<div class="flex flex-col gap-1 text-sm">
					<span class="font-medium">Operator</span>
					<div class="flex">
						<button
							type="button"
							aria-pressed={!dialogExclude}
							class="flex-1 rounded-l-md border px-3 py-2 text-sm transition-colors {!dialogExclude
								? 'z-10 border-primary bg-primary font-semibold text-primary-foreground'
								: 'border-input text-muted-foreground hover:text-foreground'}"
							onclick={() => (dialogExclude = false)}
						>
							<span class="font-mono">=</span> Equals
						</button>
						<button
							type="button"
							aria-pressed={dialogExclude}
							class="-ml-px flex-1 rounded-r-md border px-3 py-2 text-sm transition-colors {dialogExclude
								? 'z-10 border-primary bg-primary font-semibold text-primary-foreground'
								: 'border-input text-muted-foreground hover:text-foreground'}"
							onclick={() => (dialogExclude = true)}
						>
							<span class="font-mono">≠</span> Not equals
						</button>
					</div>
				</div>
				<label class="flex flex-col gap-1 text-sm">
					<span class="font-medium">Value</span>
					<Input
						placeholder="backend-service"
						bind:value={dialogValue}
						onkeydown={handleDialogKeydown}
					/>
				</label>
				{#if dialogError}
					<p class="text-xs text-red-500">{dialogError}</p>
				{/if}
			</div>
			<AlertDialog.Footer>
				<Button variant="outline" onclick={() => (addFilterOpen = false)}>Cancel</Button>
				<Button onclick={submitDialogFilter}>
					{#if dialogEditIndex !== null}
						<Check class="h-4 w-4" /> Update filter
					{:else}
						<Plus class="h-4 w-4" /> Add filter
					{/if}
				</Button>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>

	<div class="overflow-hidden rounded-md border">
		<div
			bind:this={scrollEl}
			bind:clientHeight={viewportH}
			onscroll={() => (scrollTop = scrollEl?.scrollTop ?? 0)}
			class="overflow-auto [&_[data-slot=table-container]]:overflow-visible"
		>
		<Table.Root>
			{#if loading}
				<Table.Body>
					<Table.Row>
						<Table.Cell colspan={5} class="h-48">
							<div class="flex h-full items-center justify-center">
								<LoadingCircle size="xlg" />
							</div>
						</Table.Cell>
					</Table.Row>
				</Table.Body>
			{:else if error}
				<Table.Body>
					<Table.Row>
						<Table.Cell colspan={5} class="h-24 text-center text-red-500">
							{error}
						</Table.Cell>
					</Table.Row>
				</Table.Body>
			{:else if logs.length === 0}
				<Table.Body>
					<TableEmptyState colspan={5} message="No logs found." />
				</Table.Body>
			{:else}
				<!-- Solid thead bg so rows can't show through the translucent th
				     bg-muted/60 while it is sticky; the th keeps its own darker
				     background on top, same look as the endpoints table. -->
				<Table.Header class="sticky top-0 z-10 bg-background">
					<Table.Row>
						<TracewayTableHeader
							label="Timestamp"
							tooltip="Log event time"
							class="w-[180px]"
							sortField="timestamp"
							currentSortField={sortField}
							{sortDirection}
							onSort={handleSort}
						/>
						<TracewayTableHeader
							label="Level"
							tooltip="Log severity"
							class="w-[90px]"
							sortField="severity_number"
							currentSortField={sortField}
							{sortDirection}
							onSort={handleSort}
						/>
						<TracewayTableHeader label="Message" tooltip="Log body (click row to expand)" />
						<TracewayTableHeader
							label="Service"
							tooltip="Emitting service"
							class="w-[160px]"
							sortField="service_name"
							currentSortField={sortField}
							{sortDirection}
							onSort={handleSort}
						/>
						<TracewayTableHeader
							label="Trace"
							tooltip="Linked trace ID (if present)"
							class="w-[110px]"
						/>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#if topPad > 0}
						<tr aria-hidden="true" style="height: {topPad}px"></tr>
					{/if}
					{#each visibleLogs as log (log.id)}
						<Table.Row
							data-log-id={log.id}
							class="group cursor-pointer hover:bg-muted/50"
							onclick={() => toggleExpanded(log.id)}
						>
							<Table.Cell class="text-muted-foreground tabular-nums">
								{formatDateTime(log.timestamp, { timezone })}
							</Table.Cell>
							<Table.Cell>
								<SeverityBadge
									severityText={log.severityText}
									severityNumber={log.severityNumber}
								/>
							</Table.Cell>
							<Table.Cell class="max-w-[600px] truncate font-mono text-sm">
								<LogMessage body={log.body} attributes={log.logAttributes} />
							</Table.Cell>
							<Table.Cell class="truncate text-muted-foreground">
								{log.serviceName || '—'}
							</Table.Cell>
							<Table.Cell class="font-mono text-xs">
								{#if log.traceId}
									<span class="text-muted-foreground" title={log.traceId}>
										{log.traceId.slice(0, 8)}…
									</span>
								{:else}
									<span class="text-muted-foreground">—</span>
								{/if}
							</Table.Cell>
						</Table.Row>
						{#if expandedId === log.id}
							<ExpandedLogRow
								{log}
								colspan={5}
								filterStateFor={attributeFilterState}
								onFilterToggle={toggleAttributeFilter}
								metaFilterStateFor={metaFilterState}
								onMetaFilterToggle={toggleMetaFilter}
							/>
						{/if}
					{/each}
					{#if bottomPad > 0}
						<tr aria-hidden="true" style="height: {bottomPad}px"></tr>
					{/if}
					<Table.Row class="hover:bg-transparent">
						<Table.Cell colspan={5}>
							<div class="flex items-center justify-center gap-3 py-1">
								{#if logs.length < total}
									<Button variant="outline" size="sm" onclick={loadMore} disabled={loadingMore}>
										{loadingMore ? 'Loading…' : `Load ${pageSize} more`}
									</Button>
								{/if}
								{#if loadMoreError}
									<span class="text-xs text-red-500">{loadMoreError}</span>
								{/if}
								<span class="text-xs text-muted-foreground">
									Showing {logs.length.toLocaleString()} of {total.toLocaleString()} logs
								</span>
							</div>
						</Table.Cell>
					</Table.Row>
				</Table.Body>
			{/if}
		</Table.Root>
		</div>
	</div>
</div>
