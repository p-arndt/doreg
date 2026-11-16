<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import { bytes, fraction } from '#lib/format.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Command from '#lib/components/Command.svelte';
	import Input from '#lib/components/Input.svelte';
	import Select from '#lib/components/Select.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import Readout from '#lib/components/Readout.svelte';
	import RepoTable from '#lib/features/RepoTable.svelte';
	import type { RepoListItem, Stats } from '#lib/types.js';

	let repos = $state<RepoListItem[] | null>(null);
	let stats = $state<Stats | null>(null);
	let error = $state<unknown>(null);

	const query = $derived(page.url.searchParams.get('q') ?? '');
	const sort = $derived(page.url.searchParams.get('sort') ?? 'name');
	const project = $derived(page.url.searchParams.get('project') ?? '');
	const projects = $derived(
		[...new Set((repos ?? []).map((r) => r.project ?? ''))].filter(Boolean).sort()
	);

	async function load() {
		error = null;
		try {
			[repos, stats] = await Promise.all([
				api.get<RepoListItem[]>('/api/repos'),
				api.get<Stats>('/api/stats')
			]);
		} catch (e) {
			error = e;
		}
	}
	load();

	const shown = $derived.by(() => {
		if (!repos) return [];
		const q = query.trim().toLowerCase();
		const list = repos.filter(
			(r) => (!q || r.name.includes(q)) && (!project || r.project === project)
		);
		if (sort === 'size') list.sort((a, b) => b.size - a.size);
		else if (sort === 'recent') list.sort((a, b) => b.lastPush - a.lastPush);
		return list;
	});

	function setParam(key: string, value: string, def: string) {
		const url = new URL(page.url.href);
		if (value === def) url.searchParams.delete(key);
		else url.searchParams.set(key, value);
		goto(url, { replace: true, reset: false });
	}

	const host = location.host;
	const diskUsed = $derived(stats?.disk ? stats.disk.total - stats.disk.free : 0);
</script>

<svelte:head><title>Repositories · doreg</title></svelte:head>

<PageHeader title="Repositories" description={`Images pushed to ${host}.`}>
	{#snippet actions()}
		<div class="login-cmd"><Command command={`docker login ${host}`} /></div>
	{/snippet}
</PageHeader>

{#if stats}
	<section class="storage" aria-label="Storage">
		<div class="storage__main">
			<div class="storage__head">
				<div class="storage__lead">
					<p class="storage__label">Image data</p>
					<p class="storage__value"><Readout value={bytes(stats.blobBytes)} /></p>
				</div>
				{#if stats.disk}
					<div class="storage__lead">
						<p class="storage__label">Disk free</p>
						<p class="storage__value">
							<Readout value={bytes(stats.disk.free)} />
							<span class="storage__of">of {bytes(stats.disk.total)}</span>
						</p>
					</div>
				{/if}
			</div>
			{#if stats.disk}
				<div class="storage__disk">
					<span
						class="storage__bar"
						role="meter"
						aria-label="Disk used"
						aria-valuemin={0}
						aria-valuemax={stats.disk.total}
						aria-valuenow={diskUsed}
					>
						<span
							class="storage__images"
							style:width="{fraction(stats.blobBytes, stats.disk.total) * 100}%"
						></span>
						<span
							class="storage__other"
							style:width="{fraction(diskUsed - stats.blobBytes, stats.disk.total) * 100}%"
						></span>
					</span>
					<span class="legend">
						<span><span class="key key--images"></span>Images {bytes(stats.blobBytes)}</span>
						<span
							><span class="key key--other"></span>Other files {bytes(
								Math.max(0, diskUsed - stats.blobBytes)
							)}</span
						>
						<span><span class="key key--free"></span>Free {bytes(stats.disk.free)}</span>
					</span>
				</div>
			{/if}
		</div>
		<dl class="storage__aside">
			<div>
				<dt>Repositories</dt>
				<dd class="measure">{stats.repositories}</dd>
			</div>
			<div>
				<dt>Tags</dt>
				<dd class="measure">{stats.tags}</dd>
			</div>
			<div>
				<dt>{stats.lastGc?.dryRun ? 'Last preview' : 'Last cleanup'}</dt>
				<dd>
					{#if stats.gcRunning}
						<Badge tone="accent">Running</Badge>
					{:else if stats.lastGc}
						<span class="measure">{bytes(stats.lastGc.freedBytes)}</span>
						<span class="muted"
							><Time unix={stats.lastGc.finishedAt || stats.lastGc.startedAt} /></span
						>
					{:else}
						<span class="muted">None yet</span>
					{/if}
				</dd>
			</div>
		</dl>
	</section>
{/if}

<div class="toolbar">
	<Input
		type="search"
		icon="search"
		placeholder="Filter repositories"
		aria-label="Filter repositories"
		value={query}
		oninput={(e) => setParam('q', e.currentTarget.value, '')}
		class="toolbar__search"
	/>
	{#if projects.length > 1}
		<Select
			aria-label="Project"
			value={project}
			onchange={(v) => setParam('project', v, '')}
			options={[
				{ value: '', label: 'All projects' },
				...projects.map((p) => ({ value: p, label: p }))
			]}
		/>
	{/if}
	<Select
		aria-label="Sort"
		value={sort}
		onchange={(v) => setParam('sort', v, 'name')}
		options={[
			{ value: 'name', label: 'Sort by name' },
			{ value: 'recent', label: 'Recently pushed' },
			{ value: 'size', label: 'Largest first' }
		]}
	/>
</div>

{#if error}
	<ErrorState {error} retry={load} />
{:else if !repos}
	<Skeleton rows={6} columns={[4, 1, 1, 2]} />
{:else if repos.length === 0}
	<EmptyState
		icon="box"
		title="No images yet"
		description="Tag an image with this registry’s host and push it. It shows up here right away."
	>
		<Command command={`docker push ${host}/my-app:latest`} />
	</EmptyState>
{:else if shown.length === 0}
	<EmptyState icon="search" title="No repository matches these filters">
		<Button size="sm" onclick={() => goto('/', { replace: true, reset: false })}
			>Clear filters</Button
		>
	</EmptyState>
{:else}
	<RepoTable repos={shown} />
{/if}

<style>
	.login-cmd {
		width: min(22rem, 100%);
	}

	/* One storage panel: the two sizes the screen is about, the disk as a full-width bar, the counts as a quiet aside. */
	.storage {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		gap: var(--space-5) var(--space-6);
		margin: 0 0 var(--space-6);
		padding: var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-xl);
		background: var(--surface);
		box-shadow: var(--shadow-control);
	}

	.storage__main {
		display: grid;
		align-content: space-between;
		gap: var(--space-5);
		min-width: 0;
	}

	.storage__head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: var(--space-4) var(--space-7);
	}

	.storage__lead {
		display: grid;
		gap: var(--space-1);
	}

	.storage__label {
		color: var(--fg-subtle);
		font-size: var(--text-sm);
		font-weight: 500;
	}

	.storage__value {
		display: flex;
		align-items: baseline;
		gap: var(--space-2);
		font-size: var(--text-xl);
		font-weight: 600;
		letter-spacing: -0.02em;
		line-height: 1.2;
	}

	.storage__of {
		color: var(--fg-subtle);
		font-size: var(--text-sm);
		font-weight: 400;
		letter-spacing: 0;
		font-variant-numeric: tabular-nums;
	}

	.storage__aside {
		display: grid;
		align-content: center;
		gap: var(--space-1);
		width: 15rem;
		padding-left: var(--space-6);
		border-left: 1px solid var(--border);
		font-size: var(--text-sm);
	}

	.storage__aside > div {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-6);
		min-height: 1.5rem;
	}

	.storage__aside dt {
		color: var(--fg-subtle);
	}

	.storage__aside dd {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin: 0;
		color: var(--fg);
	}

	.storage__disk {
		display: grid;
		gap: var(--space-3);
	}

	.storage__bar {
		display: flex;
		gap: 2px;
		height: 0.5rem;
		overflow: hidden;
		border-radius: var(--radius-pill);
		background: var(--track);
	}

	.storage__images {
		min-width: 4px;
		border-radius: var(--radius-pill);
		background: var(--accent);
	}

	.storage__other {
		border-radius: var(--radius-pill);
		background: var(--track-strong);
	}

	.legend {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-5);
		color: var(--fg-muted);
		font-size: var(--text-sm);
		font-variant-numeric: tabular-nums;
	}

	.legend > span {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
	}

	.key {
		width: 0.5rem;
		height: 0.5rem;
		border-radius: var(--radius-pill);
	}

	.key--images {
		background: var(--accent);
	}

	.key--other {
		background: var(--track-strong);
	}

	.key--free {
		background: var(--track);
		box-shadow: inset 0 0 0 1px var(--border-hover);
	}

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

	@media (max-width: 40rem) {
		.storage {
			padding: var(--space-4);
		}
	}

	@media (max-width: 48rem) {
		.storage {
			grid-template-columns: minmax(0, 1fr);
		}

		.storage__aside {
			width: 100%;
			padding: var(--space-3) 0 0;
			border-left: 0;
			border-top: 1px solid var(--border);
		}
	}
</style>
