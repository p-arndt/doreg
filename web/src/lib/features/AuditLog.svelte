<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import { actionLabel, when } from '#lib/format.js';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Input from '#lib/components/Input.svelte';
	import Select from '#lib/components/Select.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { AuditEntry } from '#lib/types.js';

	// Audit entries, newest first. Filters live in the URL (?actor=&action=&q=, plus ?project=
	// on the global page); `project` pins the log to one project.
	let { project, projects = [] }: { project?: string; projects?: string[] } = $props();

	const pageSize = 50;
	// Actions the backend records (internal/api, internal/events).
	const actions = [
		'push',
		'pull',
		'delete_tag',
		'delete_manifest',
		'delete_repository',
		'repository_update',
		'quota_exceeded',
		'gc_completed',
		'login',
		'login_failed',
		'project_create',
		'project_update',
		'project_delete',
		'member_add',
		'member_update',
		'member_remove',
		'robot_create',
		'robot_update',
		'robot_rotate_secret',
		'robot_delete',
		'webhook_save',
		'webhook_delete',
		'cli_secret_create',
		'cli_secret_delete'
	];

	const params = $derived(page.url.searchParams);
	const filters = $derived({
		project: project ?? params.get('project') ?? '',
		actor: params.get('actor') ?? '',
		action: params.get('action') ?? '',
		q: params.get('q') ?? ''
	});

	let entries = $state<AuditEntry[] | null>(null);
	let error = $state<unknown>(null);
	let more = $state(false);
	let loadingMore = $state(false);
	let actorInput = $state('');
	let searchInput = $state('');

	function query(before = 0) {
		const parts = Object.entries(filters)
			.filter(([, v]) => v)
			.map(([k, v]) => `${k}=${encodeURIComponent(v)}`);
		if (before) parts.push(`before=${before}`);
		parts.push(`limit=${pageSize}`);
		return `/api/audit?${parts.join('&')}`;
	}

	$effect(() => {
		const url = query();
		actorInput = filters.actor;
		searchInput = filters.q;
		entries = null;
		error = null;
		api.get<AuditEntry[]>(url).then(
			(list) => {
				entries = list;
				more = list.length === pageSize;
			},
			(e) => (error = e)
		);
	});

	async function loadMore() {
		if (!entries?.length) return;
		loadingMore = true;
		try {
			const next = await api.get<AuditEntry[]>(query(entries[entries.length - 1].id));
			entries = [...entries, ...next];
			more = next.length === pageSize;
		} catch (e) {
			error = e;
		} finally {
			loadingMore = false;
		}
	}

	function setParam(key: string, value: string) {
		const url = new URL(page.url.href);
		if (value) url.searchParams.set(key, value);
		else url.searchParams.delete(key);
		goto(url, { replace: true, reset: false });
	}

	// Text filters apply after a pause, so each keystroke does not reload the log.
	let timer: ReturnType<typeof setTimeout>;
	function debounced(key: string, value: string) {
		clearTimeout(timer);
		timer = setTimeout(() => setParam(key, value.trim()), 350);
	}

	const filtered = $derived(
		!!(filters.actor || filters.action || filters.q || (!project && filters.project))
	);

	function clearFilters() {
		const url = new URL(page.url.href);
		for (const k of ['actor', 'action', 'q', ...(project ? [] : ['project'])])
			url.searchParams.delete(k);
		goto(url, { replace: true, reset: false });
	}

	const kindTone = {
		user: 'neutral',
		robot: 'accent',
		anonymous: 'warning',
		system: 'neutral'
	} as const;
	const badActions = new Set(['login_failed', 'quota_exceeded']);
</script>

<div class="toolbar">
	<Input
		type="search"
		icon="search"
		placeholder="Search resources and details"
		aria-label="Search the audit log"
		bind:value={searchInput}
		oninput={(e) => debounced('q', e.currentTarget.value)}
		class="toolbar__search"
	/>
	<Input
		placeholder="Actor"
		aria-label="Filter by actor"
		bind:value={actorInput}
		oninput={(e) => debounced('actor', e.currentTarget.value)}
		class="toolbar__actor"
	/>
	<Select
		aria-label="Filter by action"
		value={filters.action}
		onchange={(v) => setParam('action', v)}
		options={[
			{ value: '', label: 'All actions' },
			...actions.map((a) => ({ value: a, label: actionLabel(a) }))
		]}
	/>
	{#if !project && projects.length}
		<Select
			aria-label="Filter by project"
			value={filters.project}
			onchange={(v) => setParam('project', v)}
			options={[
				{ value: '', label: 'All projects' },
				...projects.map((p) => ({ value: p, label: p }))
			]}
		/>
	{/if}
</div>

{#if error}
	<ErrorState {error} />
{:else if !entries}
	<Skeleton rows={8} columns={[1, 1, 1, 3]} />
{:else if entries.length === 0}
	<EmptyState
		icon="list"
		title={filtered ? 'No entries match these filters' : 'Nothing recorded yet'}
		description={filtered
			? undefined
			: 'Pushes, deletions, sign-ins and settings changes show up here.'}
	>
		{#if filtered}<Button size="sm" onclick={clearFilters}>Clear filters</Button>{/if}
	</EmptyState>
{:else}
	<Table label="Audit log">
		<thead>
			<tr>
				<th>Time</th>
				<th>Actor</th>
				<th>Action</th>
				<th>Resource</th>
				{#if !project}<th class="col-project">Project</th>{/if}
				<th class="col-ip">IP</th>
			</tr>
		</thead>
		<tbody>
			{#each entries as e (e.id)}
				<tr>
					<td class="muted nowrap" title={when(e.at)}><Time unix={e.at} /></td>
					<td class="nowrap">
						<span class="actor">
							{e.actor || e.actorKind}
							{#if e.actorKind !== 'user' && e.actor}<Badge tone={kindTone[e.actorKind]}
									>{e.actorKind}</Badge
								>{/if}
						</span>
					</td>
					<td class="nowrap">
						<span class:bad={badActions.has(e.action)}>{actionLabel(e.action)}</span>
					</td>
					<td class="resource">
						<span>{e.resource}</span>
						{#if e.detail}<span class="detail">{e.detail}</span>{/if}
					</td>
					{#if !project}
						<td class="col-project">
							{#if e.project}<a class="muted" href="/projects/{e.project}">{e.project}</a>{/if}
						</td>
					{/if}
					<td class="col-ip subtle mono nowrap">{e.ip}</td>
				</tr>
			{/each}
		</tbody>
	</Table>
	{#if more}
		<div class="more">
			<Button loading={loadingMore} onclick={loadMore}>Load older entries</Button>
		</div>
	{/if}
{/if}

<style>
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-bottom: var(--space-3);
	}

	.toolbar :global(.toolbar__search) {
		flex: 1 1 14rem;
		max-width: 22rem;
	}

	.toolbar :global(.toolbar__actor) {
		width: 10rem;
	}

	.actor {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
	}

	.bad {
		color: var(--danger);
	}

	.resource {
		min-width: 12rem;
		overflow-wrap: anywhere;
	}

	.resource span {
		display: block;
	}

	.detail {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	.more {
		display: flex;
		justify-content: center;
		margin-top: var(--space-4);
	}

	@media (max-width: 56rem) {
		.col-ip,
		.col-project {
			display: none;
		}
	}
</style>
