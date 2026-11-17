<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, q } from '#lib/api.js';
	import { bytes, count, plural, projectOf, refPath, shortDigest } from '#lib/format.js';
	import { hasProjectRole, repoCrumbs } from '#lib/session.svelte.js';
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
	import Readout from '#lib/components/Readout.svelte';
	import Markdown from '#lib/components/Markdown.svelte';
	import Notice from '#lib/components/Notice.svelte';
	import Field from '#lib/components/Field.svelte';
	import SegmentedControl from '#lib/components/SegmentedControl.svelte';
	import type { ProjectRole, Repo } from '#lib/types.js';
	import { defaultView, isGitHubUrl, sourceLabel } from '#lib/readme.js';
	import { latestTag } from '#lib/imageConfig.js';
	import LatestPush from './LatestPush.svelte';

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
	// Overview is offered to readers when there is a README, and to writers so they can start one.
	const showOverview = $derived(!!repo && (!!repo.readme || canDelete));
	const requestedView = $derived(page.url.searchParams.get('view'));
	const view = $derived(requestedView ?? defaultView(!!repo?.readme));
	// A ?view=overview link for a reader without a README falls back to the tags.
	const current = $derived(view === 'overview' && !showOverview ? 'tags' : view);
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

	const latest = $derived(repo ? latestTag(repo.tags) : undefined);

	const tabs = $derived([
		...(showOverview ? [{ key: 'overview', label: 'Overview', href: '?view=overview' }] : []),
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

	let editing = $state(false);
	let draft = $state('');
	let editMode = $state<'write' | 'preview'>('write');
	let previewHtml = $state('');
	let previewError = $state('');
	let previewing = $state(false);
	let saving = $state(false);
	let importOpen = $state(false);
	let importUrl = $state('');
	let importing = $state(false);
	let importError = $state('');
	let importedUnsaved = $state(false);

	const dirty = $derived(editing && draft !== (repo?.readme ?? ''));

	function messageOf(e: unknown) {
		return e instanceof Error ? e.message : String(e);
	}

	function startEditing(openImport = false) {
		if (!repo) return;
		draft = repo.readme;
		editMode = 'write';
		previewError = '';
		importError = '';
		importedUnsaved = false;
		// The source repository is the natural default to import from when it lives on GitHub.
		if (isGitHubUrl(repo.source)) importUrl = repo.source;
		importOpen = openImport;
		editing = true;
	}

	function cancelEditing() {
		if (dirty && !confirm('Discard your unsaved README changes?')) return;
		editing = false;
		importedUnsaved = false;
	}

	function setMode(mode: 'write' | 'preview') {
		editMode = mode;
		if (mode === 'preview') showPreview();
	}

	async function showPreview() {
		previewing = true;
		previewError = '';
		try {
			previewHtml = (
				await api.post<{ html: string }>('/api/repo/readme/preview', { readme: draft })
			).html;
		} catch (e) {
			previewError = messageOf(e);
		} finally {
			previewing = false;
		}
	}

	async function importReadme() {
		importing = true;
		importError = '';
		try {
			const res = await api.post<{ readme: string; source: string }>(
				`/api/repo/readme/github?name=${q(name)}`,
				{ source: importUrl.trim() }
			);
			draft = res.readme;
			importOpen = false;
			importedUnsaved = true;
			editMode = 'preview';
			await showPreview();
		} catch (e) {
			importError = messageOf(e);
		} finally {
			importing = false;
		}
	}

	async function saveReadme() {
		saving = true;
		try {
			const res = await api.put<{ readme: string; readmeHtml: string }>(
				`/api/repo/readme?name=${q(name)}`,
				{ readme: draft }
			);
			if (repo) {
				repo.readme = res.readme;
				repo.readmeHtml = res.readmeHtml;
			}
			editing = false;
			importedUnsaved = false;
			notify('README saved.');
		} catch (e) {
			fail(e);
		} finally {
			saving = false;
		}
	}
</script>

<svelte:head><title>{name} · doreg</title></svelte:head>

<PageHeader title={name} crumbs={repoCrumbs(project)} description={repo?.description || undefined}>
	{#snippet meta()}
		{#if repo?.public}<Badge tone="accent" title="Anyone can pull without signing in">Public</Badge
			>{/if}
		{#if repo && sourceLabel(repo.source)}
			<span class="source">
				Source:
				<a href={repo.source} target="_blank" rel="noopener noreferrer"
					>{sourceLabel(repo.source)}</a
				>
			</span>
		{/if}
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
			<Fact label="Stored" big><Readout value={bytes(repo.size)} /></Fact>
			<Fact label="Pulls" measure
				><span title={repo.pulls.toLocaleString()}>{count(repo.pulls)}</span></Fact
			>
			<Fact label="Created"><Time unix={repo.createdAt} /></Fact>
		</Facts>
	</div>
{/if}

<Tabs label="Repository sections" items={tabs} active={current} />

{#if error}
	<ErrorState {error} retry={load} />
{:else if !repo}
	<Skeleton rows={5} columns={[2, 1, 2, 1, 1]} />
{:else if current === 'overview'}
	{#if editing}
		<div class="editor">
			<div class="editor__bar">
				<SegmentedControl
					label="Editor mode"
					value={editMode}
					options={[
						{ value: 'write', label: 'Write' },
						{ value: 'preview', label: 'Preview' }
					]}
					onchange={setMode}
				/>
				<Button size="sm" aria-expanded={importOpen} onclick={() => (importOpen = !importOpen)}
					>Import from GitHub</Button
				>
			</div>

			{#if importOpen}
				<form
					class="import"
					onsubmit={(e) => {
						e.preventDefault();
						importReadme();
					}}
				>
					<Field label="GitHub repository">
						<Input
							bind:value={importUrl}
							placeholder="https://github.com/owner/repo"
							aria-label="GitHub repository"
							mono
						/>
					</Field>
					<Button type="submit" loading={importing}>Import</Button>
				</form>
				{#if importError}<Notice tone="danger">{importError}</Notice>{/if}
			{/if}

			{#if importedUnsaved}
				<Notice tone="warning">
					Imported from GitHub. Nothing is saved yet: check the preview and click Save.
				</Notice>
			{/if}

			{#if editMode === 'write'}
				<textarea
					class="editor__text"
					bind:value={draft}
					rows="20"
					spellcheck="false"
					aria-label="README source in Markdown"></textarea>
			{:else if previewing}
				<p class="muted">Rendering preview…</p>
			{:else if previewError}
				<Notice tone="danger">{previewError}</Notice>
			{:else if !draft.trim()}
				<p class="muted">Nothing to preview yet.</p>
			{:else}
				<div class="editor__preview">
					<Markdown html={previewHtml} />
				</div>
			{/if}

			<div class="editor__actions">
				<Button variant="primary" loading={saving} onclick={saveReadme}>Save</Button>
				<Button onclick={cancelEditing}>Cancel</Button>
			</div>
		</div>
	{:else if repo.readme}
		<div class="overview">
			{#if canDelete}
				<div class="overview__bar">
					<Button size="sm" onclick={() => startEditing()}>Edit</Button>
				</div>
			{/if}
			<Markdown html={repo.readmeHtml} />
		</div>
	{:else}
		<EmptyState
			icon="list"
			title="No README yet"
			description="Describe what this repository holds and how to pull it. Everyone who can read the repository sees it here."
		>
			<Button variant="primary" onclick={() => startEditing()}>Write README</Button>
			<Button onclick={() => startEditing(true)}>Import from GitHub</Button>
		</EmptyState>
	{/if}
{:else if current === 'tags'}
	{#if repo.tags.length === 0}
		<EmptyState
			icon="box"
			title="No tags"
			description="Everything here is untagged and will be removed by the next garbage collection."
		/>
	{:else}
		{#if latest}<LatestPush repo={name} tag={latest} />{/if}
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
					<th class="right col-pushed">Pulls</th>
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
						<td class="right muted nowrap col-pushed" title={t.manifest.pulls.toLocaleString()}
							>{count(t.manifest.pulls)}</td
						>
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
{:else if current === 'untagged'}
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
{:else if current === 'settings' && canDelete}
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

	.source {
		display: inline-flex;
		gap: var(--space-1);
	}

	.source a {
		color: var(--fg);
		text-decoration: underline;
		text-decoration-color: var(--border-strong);
		text-underline-offset: 3px;
	}

	.source a:hover {
		text-decoration-color: var(--fg);
	}

	.overview {
		display: grid;
		gap: var(--space-3);
	}

	.overview__bar {
		display: flex;
		justify-content: flex-end;
	}

	.editor {
		display: grid;
		gap: var(--space-3);
		max-width: 48rem;
	}

	.editor__bar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.import {
		display: flex;
		align-items: flex-end;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.import :global(.field) {
		flex: 1 1 18rem;
	}

	.editor__text {
		width: 100%;
		min-height: 20rem;
		padding: var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius);
		background: var(--bg-sidebar);
		color: var(--fg);
		box-shadow: var(--shadow-control);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		line-height: 1.5;
		resize: vertical;
	}

	.editor__text:focus-visible {
		outline: 2px solid var(--border-focus);
		outline-offset: 1px;
	}

	.editor__preview {
		padding: var(--space-3) 0;
	}

	.editor__actions {
		display: flex;
		gap: var(--space-2);
	}

	.toolbar {
		display: flex;
		gap: var(--space-2);
		margin-bottom: var(--space-3);
	}

	.toolbar :global(.toolbar__search) {
		flex: 1 1 14rem;
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
		background: var(--surface);
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
