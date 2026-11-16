<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, q } from '#lib/api.js';
	import { bytes, plural, projectOf, refPath, shortDigest } from '#lib/format.js';
	import { hasProjectRole } from '#lib/session.svelte.js';
	import { loadProjectRole } from '#lib/projectRole.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import Section from '#lib/components/Section.svelte';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Input from '#lib/components/Input.svelte';
	import Checkbox from '#lib/components/Checkbox.svelte';
	import PullReference from '#lib/components/PullReference.svelte';
	import PullCopyButton from '#lib/components/PullCopyButton.svelte';
	import ConfirmButton from '#lib/components/ConfirmButton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Platforms from '#lib/components/Platforms.svelte';
	import Time from '#lib/components/Time.svelte';
	import Facts from '#lib/components/Facts.svelte';
	import Fact from '#lib/components/Fact.svelte';
	import type { ProjectRole, Repo } from '#lib/types.js';

	let { name }: { name: string } = $props();
	let repo = $state<Repo | null>(null);
	let error = $state<unknown>(null);
	let confirmName = $state('');
	let deleting = $state(false);

	const host = location.host;
	const project = $derived(projectOf(name));
	let role = $state<ProjectRole>('');
	$effect(() => {
		loadProjectRole(project).then((r) => (role = r));
	});
	const canDelete = $derived(hasProjectRole(role, 'write'));
	const canManage = $derived(hasProjectRole(role, 'admin'));
	const view = $derived(page.url.searchParams.get('view') ?? 'tags');
	const filter = $derived(page.url.searchParams.get('q') ?? '');

	async function load() {
		error = null;
		try {
			repo = await api.get<Repo>(`/api/repo?name=${q(name)}`);
		} catch (e) {
			error = e;
		}
	}
	load();

	const tags = $derived(
		repo
			? repo.tags.filter((t) => !filter || t.name.toLowerCase().includes(filter.toLowerCase()))
			: []
	);

	const tabs = $derived([
		{ key: 'tags', label: 'Tags', href: '?view=tags', count: repo?.tags.length },
		{ key: 'untagged', label: 'Untagged', href: '?view=untagged', count: repo?.untagged.length },
		...(canDelete ? [{ key: 'settings', label: 'Settings', href: '?view=settings' }] : [])
	]);

	function setParam(key: string, value: string, def: string) {
		const url = new URL(page.url.href);
		if (value === def) url.searchParams.delete(key);
		else url.searchParams.set(key, value);
		goto(url, { replace: key === 'q', reset: false });
	}

	async function deleteTag(tag: string) {
		try {
			await api.del(`/api/tag?repo=${q(name)}&tag=${q(tag)}`);
			notify(`Deleted tag ${tag}. Garbage collection frees the space.`);
			await load();
		} catch (e) {
			fail(e);
		}
	}

	async function deleteManifest(digest: string) {
		try {
			await api.del(`/api/manifest?repo=${q(name)}&digest=${q(digest)}`);
			notify(`Deleted ${shortDigest(digest)}.`);
			await load();
		} catch (e) {
			fail(e);
		}
	}

	async function setPublic(pub: boolean) {
		try {
			await api.patch(`/api/repo?name=${q(name)}`, { public: pub });
			if (repo) repo.public = pub;
			notify(pub ? 'Anyone can now pull this repository.' : 'Pulling now requires an account.');
		} catch (e) {
			fail(e);
		}
	}

	async function deleteRepo() {
		deleting = true;
		try {
			await api.del(`/api/repo?name=${q(name)}`);
			notify(`Deleted ${name}. Garbage collection frees the space.`);
			await goto('/');
		} catch (e) {
			fail(e);
		} finally {
			deleting = false;
		}
	}
</script>

<svelte:head><title>{name} · doreg</title></svelte:head>

<PageHeader
	title={name}
	crumbs={[
		{ href: '/projects', label: 'Projects' },
		{ href: `/projects/${project}`, label: project }
	]}
>
	{#snippet meta()}
		{#if repo?.public}<Badge tone="accent" title="Anyone can pull without signing in">Public</Badge
			>{/if}
	{/snippet}
	{#snippet actions()}
		<div class="pull-cmd"><PullReference reference={`${host}/${name}`} /></div>
	{/snippet}
</PageHeader>

{#if repo}
	<div class="facts-row">
		<Facts label="Repository">
			<Fact label="Tags" measure>{repo.tags.length}</Fact>
			<Fact label="Untagged" measure>{repo.untagged.length}</Fact>
			<Fact label="Stored" measure>{bytes(repo.size)}</Fact>
			<Fact label="Created"><Time unix={repo.createdAt} /></Fact>
		</Facts>
	</div>
{/if}

<Tabs label="Repository sections" items={tabs} active={view} />

{#if error}
	<ErrorState {error} retry={load} />
{:else if !repo}
	<Skeleton rows={5} columns={[2, 1, 2, 1, 1]} />
{:else if view === 'tags'}
	{#if repo.tags.length === 0}
		<EmptyState
			icon="box"
			title="No tags"
			description="Everything here is untagged and will be removed by the next garbage collection."
		/>
	{:else}
		<div class="toolbar">
			<Input
				type="search"
				icon="search"
				placeholder="Filter tags"
				aria-label="Filter tags"
				value={filter}
				oninput={(e) => setParam('q', e.currentTarget.value, '')}
				class="toolbar__search"
			/>
		</div>
		<Table label="Tags">
			<thead>
				<tr>
					<th>Tag</th>
					<th class="col-platforms">Platforms</th>
					<th class="right">Size</th>
					<th class="col-pushed">Pushed</th>
					<th class="shrink"><span class="visually-hidden">Actions</span></th>
				</tr>
			</thead>
			<tbody>
				{#each tags as t (t.name)}
					<tr>
						<td>
							<div class="tag">
								<a class="row-link" href={refPath(name, t.name)}>{t.name}</a>
								<span class="digest" title={t.manifest.digest}
									>{shortDigest(t.manifest.digest)}</span
								>
							</div>
						</td>
						<td class="col-platforms"><Platforms m={t.manifest} /></td>
						<td class="right muted nowrap">{bytes(t.manifest.size)}</td>
						<td class="muted nowrap col-pushed"><Time unix={t.updatedAt} /></td>
						<td class="shrink">
							<span class="row-actions">
								<PullCopyButton reference={`${host}/${name}:${t.name}`} />
								{#if canDelete}
									<ConfirmButton
										label={`Delete tag ${t.name}`}
										iconOnly
										icon="trash"
										question={`Delete ${t.name}?`}
										onconfirm={() => deleteTag(t.name)}
									/>
								{/if}
							</span>
						</td>
					</tr>
				{:else}
					<tr><td colspan="5" class="muted">No tag matches “{filter}”.</td></tr>
				{/each}
			</tbody>
		</Table>
	{/if}
{:else if view === 'untagged'}
	<p class="lede">
		Manifests no tag points to, for example after a tag moved to a new build. Garbage collection
		removes them unless they were pulled by digest recently.
	</p>
	{#if repo.untagged.length === 0}
		<EmptyState
			icon="check"
			title="Nothing untagged"
			description="Every manifest is reachable from a tag."
		/>
	{:else}
		<Table label="Untagged manifests">
			<thead>
				<tr>
					<th>Digest</th>
					<th class="col-platforms">Platforms</th>
					<th class="right">Size</th>
					<th class="col-pushed">Pushed</th>
					<th class="col-pushed">Last pulled</th>
					<th class="shrink"><span class="visually-hidden">Actions</span></th>
				</tr>
			</thead>
			<tbody>
				{#each repo.untagged as m (m.digest)}
					<tr>
						<td>
							<a class="row-link mono" href={refPath(name, m.digest)} title={m.digest}
								>{shortDigest(m.digest)}</a
							>
						</td>
						<td class="col-platforms"><Platforms {m} /></td>
						<td class="right muted nowrap">{bytes(m.size)}</td>
						<td class="muted nowrap col-pushed"><Time unix={m.createdAt} /></td>
						<td class="muted nowrap col-pushed"><Time unix={m.lastPulledAt} /></td>
						<td class="shrink">
							<span class="row-actions">
								<PullCopyButton reference={`${host}/${name}@${m.digest}`} />
								{#if canDelete}
									<ConfirmButton
										label={`Delete ${shortDigest(m.digest)}`}
										iconOnly
										icon="trash"
										question="Delete now?"
										onconfirm={() => deleteManifest(m.digest)}
									/>
								{/if}
							</span>
						</td>
					</tr>
				{/each}
			</tbody>
		</Table>
	{/if}
{:else if view === 'settings' && canDelete}
	<div class="settings">
		{#if canManage}
			<Section title="Visibility">
				<div class="card">
					<Checkbox
						label="Allow anonymous pull"
						description="Anyone can pull this repository without an account. Pushing always needs one."
						checked={repo.public}
						onchange={(e) => setPublic(e.currentTarget.checked)}
					/>
				</div>
			</Section>
		{/if}

		<Section title="Danger zone">
			<div class="card card--danger">
				<div class="danger__text">
					<h3>Delete this repository</h3>
					<p class="muted">
						Removes all {plural(repo.tags.length, 'tag')} and every manifest. Layers shared with other
						repositories stay; the rest is freed by the next garbage collection.
					</p>
				</div>
				<div class="danger__form">
					<Input
						placeholder={name}
						aria-label="Type the repository name to confirm"
						bind:value={confirmName}
						mono
					/>
					<Button
						variant="danger-solid"
						disabled={confirmName !== name}
						loading={deleting}
						onclick={deleteRepo}>Delete repository</Button
					>
				</div>
				<p class="subtle hint">Type <code>{name}</code> to confirm.</p>
			</div>
		</Section>
	</div>
{/if}

<style>
	.facts-row {
		margin: calc(-1 * var(--space-2)) 0 var(--space-5);
	}

	.pull-cmd {
		width: min(24rem, 100%);
	}

	.toolbar {
		display: flex;
		gap: var(--space-2);
		margin-bottom: var(--space-3);
	}

	.toolbar :global(.toolbar__search) {
		flex: 1;
		max-width: 22rem;
	}

	.tag {
		display: flex;
		align-items: baseline;
		gap: var(--space-3);
		flex-wrap: wrap;
	}

	.lede {
		max-width: 44rem;
		margin-bottom: var(--space-4);
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.settings {
		max-width: 44rem;
	}

	.card {
		padding: var(--space-4);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
	}

	.card--danger {
		display: grid;
		gap: var(--space-3);
		border-color: var(--danger-line);
	}

	.danger__text {
		display: grid;
		gap: var(--space-1);
		font-size: var(--text-sm);
	}

	.danger__form {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.danger__form :global(.input) {
		flex: 1 1 14rem;
		max-width: 22rem;
	}

	.hint {
		font-size: var(--text-xs);
	}

	@media (max-width: 48rem) {
		.col-platforms {
			display: none;
		}
	}

	@media (max-width: 40rem) {
		.col-pushed {
			display: none;
		}
	}
</style>
