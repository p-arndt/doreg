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
	import Facts from '#lib/components/Facts.svelte';
	import Fact from '#lib/components/Fact.svelte';
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
	<div class="storage">
		<Facts label="Registry">
			<Fact label="Repositories" measure>{stats.repositories}</Fact>
			<Fact label="Tags" measure>{stats.tags}</Fact>
			<Fact label="Image data" measure>{bytes(stats.blobBytes)}</Fact>
			{#if stats.disk}
				<Fact label="Disk" measure>
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
					{bytes(stats.disk.free)} free of {bytes(stats.disk.total)}
				</Fact>
			{/if}
			<Fact label={stats.lastGc?.dryRun ? 'Last preview' : 'Last cleanup'}>
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
			</Fact>
		</Facts>
	</div>
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
			onchange={(e) => setParam('project', e.currentTarget.value, '')}
		>
			<option value="">All projects</option>
			{#each projects as p (p)}<option value={p}>{p}</option>{/each}
		</Select>
	{/if}
	<Select
		aria-label="Sort"
		value={sort}
		onchange={(e) => setParam('sort', e.currentTarget.value, 'name')}
	>
		<option value="name">Sort by name</option>
		<option value="recent">Recently pushed</option>
		<option value="size">Largest first</option>
	</Select>
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
		width: min(20rem, 100%);
	}

	.storage {
		margin: calc(-1 * var(--space-2)) 0 var(--space-6);
	}

	/* The disk as a ruled bar: image data in lime, everything else on the disk in grey. */
	.storage__bar {
		display: inline-flex;
		width: 6rem;
		height: 6px;
		margin-right: var(--space-2);
		background: var(--track);
	}

	.storage__images {
		min-width: 2px;
		background: var(--accent);
	}

	.storage__other {
		background: var(--track-strong);
	}

	.toolbar {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-bottom: var(--space-3);
	}

	.toolbar :global(.toolbar__search) {
		flex: 1;
		max-width: 22rem;
	}
</style>
