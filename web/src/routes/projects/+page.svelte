<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import { gbToBytes } from '#lib/format.js';
	import { can } from '#lib/session.svelte.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Checkbox from '#lib/components/Checkbox.svelte';
	import Field from '#lib/components/Field.svelte';
	import Input from '#lib/components/Input.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import UsageBar from '#lib/components/UsageBar.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { Project } from '#lib/types.js';

	let projects = $state<Project[] | null>(null);
	let error = $state<unknown>(null);
	let form = $state({ name: '', public: false, quotaGb: '' });
	let creating = $state(false);

	const query = $derived(page.url.searchParams.get('q') ?? '');
	const showForm = $derived(page.url.searchParams.get('new') === '1' && can.admin());
	const shown = $derived(
		(projects ?? []).filter((p) => !query || p.name.includes(query.trim().toLowerCase()))
	);

	async function load() {
		error = null;
		try {
			projects = await api.get<Project[]>('/api/projects');
		} catch (e) {
			error = e;
		}
	}
	load();

	function setParam(key: string, value: string) {
		const url = new URL(page.url.href);
		if (value) url.searchParams.set(key, value);
		else url.searchParams.delete(key);
		goto(url, { replace: key === 'q', reset: false });
	}

	async function create(e: SubmitEvent) {
		e.preventDefault();
		creating = true;
		try {
			await api.post('/api/projects', {
				name: form.name,
				public: form.public,
				quotaBytes: gbToBytes(form.quotaGb)
			});
			notify(`Created project ${form.name}. Push to ${location.host}/${form.name}/<image>.`);
			const name = form.name;
			form = { name: '', public: false, quotaGb: '' };
			await goto(`/projects/${name}`);
		} catch (err) {
			fail(err);
		} finally {
			creating = false;
		}
	}

	const roleLabel: Record<string, string> = { read: 'Read', write: 'Write', admin: 'Admin' };
</script>

<svelte:head><title>Projects · doreg</title></svelte:head>

<PageHeader
	title="Projects"
	description="A project is the first part of a repository name and owns its members, robots, webhooks and quota."
>
	{#snippet actions()}
		{#if can.admin() && !showForm}
			<Button variant="primary" icon="plus" onclick={() => setParam('new', '1')}>New project</Button
			>
		{/if}
	{/snippet}
</PageHeader>

{#if showForm}
	<form class="editor" onsubmit={create}>
		<h2>New project</h2>
		<div class="grid2">
			<Field label="Name" hint="Lowercase letters, digits and . _ - separators.">
				<Input
					bind:value={form.name}
					required
					pattern="[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*"
					placeholder="team"
					autocomplete="off"
				/>
			</Field>
			<Field label="Quota (GB)" hint="Empty means no limit.">
				<Input type="number" min={0} step="any" bind:value={form.quotaGb} placeholder="No limit" />
			</Field>
		</div>
		<Checkbox
			label="Public"
			description="Anyone can pull images from this project without an account."
			bind:checked={form.public}
		/>
		<div class="row">
			<Button variant="primary" type="submit" loading={creating}>Create project</Button>
			<Button variant="ghost" onclick={() => setParam('new', '')}>Cancel</Button>
		</div>
	</form>
{/if}

{#if projects && projects.length > 6}
	<div class="toolbar">
		<Input
			type="search"
			icon="search"
			placeholder="Filter projects"
			aria-label="Filter projects"
			value={query}
			oninput={(e) => setParam('q', e.currentTarget.value)}
			class="toolbar__search"
		/>
	</div>
{/if}

{#if error}
	<ErrorState {error} retry={load} />
{:else if !projects}
	<Skeleton rows={4} columns={[3, 1, 1, 2, 1]} />
{:else if projects.length === 0}
	<EmptyState
		icon="folder"
		title="No projects you can see"
		description="Ask an administrator to add you to a project."
	/>
{:else if shown.length === 0}
	<EmptyState icon="search" title="No project matches “{query}”">
		<Button size="sm" onclick={() => setParam('q', '')}>Clear filter</Button>
	</EmptyState>
{:else}
	<Table label="Projects">
		<thead>
			<tr>
				<th>Name</th>
				<th class="col-role">Your role</th>
				<th class="right">Repositories</th>
				<th>Storage</th>
				<th class="col-created">Created</th>
			</tr>
		</thead>
		<tbody>
			{#each shown as p (p.id)}
				<tr>
					<td>
						<span class="name">
							<a class="row-link" href="/projects/{p.name}">{p.name}</a>
							{#if p.public}<Badge title="Anyone can pull">Public</Badge>{/if}
						</span>
					</td>
					<td class="col-role muted">{roleLabel[p.role] ?? 'Public access'}</td>
					<td class="right muted">{p.repositories}</td>
					<td><UsageBar used={p.usedBytes} quota={p.quotaBytes} /></td>
					<td class="col-created muted nowrap"><Time unix={p.createdAt} /></td>
				</tr>
			{/each}
		</tbody>
	</Table>
{/if}

<style>
	.editor {
		display: grid;
		gap: var(--space-4);
		max-width: 40rem;
		margin-bottom: var(--space-6);
		padding: var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
	}

	.grid2 {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-4);
	}

	.toolbar {
		margin-bottom: var(--space-3);
	}

	.toolbar :global(.toolbar__search) {
		max-width: 22rem;
	}

	.name {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
	}

	@media (max-width: 48rem) {
		.col-created {
			display: none;
		}
	}

	@media (max-width: 40rem) {
		.col-role {
			display: none;
		}

		.grid2 {
			grid-template-columns: 1fr;
		}
	}
</style>
