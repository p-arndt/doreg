<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import { describePolicy, plural, refPath, shortDigest } from '#lib/format.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Checkbox from '#lib/components/Checkbox.svelte';
	import Field from '#lib/components/Field.svelte';
	import Input from '#lib/components/Input.svelte';
	import Notice from '#lib/components/Notice.svelte';
	import ConfirmButton from '#lib/components/ConfirmButton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { Policy, TagDeletion } from '#lib/types.js';

	const blank = (): Policy => ({
		id: 0,
		name: '',
		repoPattern: '*',
		tagPattern: '',
		keepPattern: '^(latest|main|stable)$',
		keepLast: 10,
		olderThanDays: 0,
		enabled: true
	});

	let policies = $state<Policy[] | null>(null);
	let error = $state<unknown>(null);
	let draft = $state<Policy | null>(null);
	let preview = $state<TagDeletion[] | null>(null);
	let previewError = $state('');
	let saving = $state(false);

	// "?edit=new" or "?edit=<id>" opens the editor, so it survives a reload.
	const editing = $derived(page.url.searchParams.get('edit') ?? '');

	async function load() {
		error = null;
		try {
			policies = await api.get<Policy[]>('/api/policies');
		} catch (e) {
			error = e;
		}
	}
	load();

	$effect(() => {
		const key = editing;
		if (!key) {
			draft = null;
			return;
		}
		if (key === 'new') {
			if (draft?.id !== 0) setDraft(blank());
			return;
		}
		const p = policies?.find((x) => String(x.id) === key);
		if (p && draft?.id !== p.id) setDraft({ ...p });
	});

	function setDraft(p: Policy) {
		draft = p;
		preview = null;
		previewError = '';
	}

	function openEditor(key: string) {
		const url = new URL(page.url.href);
		if (key) url.searchParams.set('edit', key);
		else url.searchParams.delete('edit');
		goto(url, { reset: false });
	}

	// Live preview of the draft: what it would delete on its own, right now.
	let timer: ReturnType<typeof setTimeout>;
	$effect(() => {
		// The preview only cares about the rules, so an unnamed draft still gets one.
		const p = draft ? { ...draft, name: draft.name || 'preview', enabled: true } : null;
		clearTimeout(timer);
		if (!p) return;
		timer = setTimeout(async () => {
			try {
				preview = await api.post<TagDeletion[]>('/api/policies/preview', [p]);
				previewError = '';
			} catch (e) {
				preview = null;
				previewError = e instanceof Error ? e.message : String(e);
			}
		}, 300);
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!draft) return;
		saving = true;
		try {
			if (draft.id) await api.put(`/api/policies/${draft.id}`, draft);
			else await api.post('/api/policies', draft);
			notify('Policy saved. It applies on the next garbage collection.');
			await load();
			openEditor('');
		} catch (err) {
			fail(err);
		} finally {
			saving = false;
		}
	}

	async function remove(p: Policy) {
		try {
			await api.del(`/api/policies/${p.id}`);
			if (editing === String(p.id)) openEditor('');
			await load();
		} catch (e) {
			fail(e);
		}
	}

	async function toggle(p: Policy) {
		try {
			await api.put(`/api/policies/${p.id}`, { ...p, enabled: !p.enabled });
			await load();
		} catch (e) {
			fail(e);
		}
	}
</script>

<svelte:head><title>Retention · doreg</title></svelte:head>

<PageHeader
	title="Retention"
	description="Delete old tags automatically. Garbage collection then frees what they used."
>
	{#snippet actions()}
		{#if !draft && policies?.length}
			<Button variant="primary" icon="plus" onclick={() => openEditor('new')}>New policy</Button>
		{/if}
	{/snippet}
</PageHeader>

{#if draft}
	<div class="editor">
		<form class="editor__form" onsubmit={save}>
			<h2>{draft.id ? 'Edit policy' : 'New policy'}</h2>
			<Field label="Name">
				<Input bind:value={draft.name} required placeholder="Feature branches" />
			</Field>
			<div class="grid2">
				<Field label="Repositories">
					{#snippet hint()}Glob; <code>*</code> also matches <code>/</code>, e.g.
						<code>team/*</code>{/snippet}
					<Input mono bind:value={draft.repoPattern} required />
				</Field>
				<Field label="Only tags matching">
					{#snippet hint()}Regular expression, e.g. <code>^pr-</code>{/snippet}
					<Input mono bind:value={draft.tagPattern} placeholder="all tags" />
				</Field>
			</div>
			<div class="grid2">
				<Field label="Keep the newest" hint="0 means no count limit.">
					<Input type="number" min={0} bind:value={draft.keepLast} />
				</Field>
				<Field label="Delete when older than (days)" hint="0 means any age.">
					<Input type="number" min={0} bind:value={draft.olderThanDays} />
				</Field>
			</div>
			<Field label="Never delete tags matching" hint="Regular expression for protected tags.">
				<Input mono bind:value={draft.keepPattern} placeholder="none protected" />
			</Field>
			<Checkbox label="Enabled" bind:checked={draft.enabled} />
			<div class="row">
				<Button variant="primary" type="submit" loading={saving} disabled={!!previewError}
					>Save policy</Button
				>
				<Button variant="ghost" onclick={() => openEditor('')}>Cancel</Button>
			</div>
		</form>

		<section class="editor__preview" aria-live="polite">
			<header class="preview__head">
				<h2>Would delete now</h2>
				{#if preview}<span class="muted num">{plural(preview.length, 'tag')}</span>{/if}
			</header>
			{#if previewError}
				<Notice tone="danger">{previewError}</Notice>
			{:else if !preview}
				<Skeleton rows={3} columns={[3, 1]} />
			{:else if preview.length === 0}
				<p class="preview__none">This policy would not delete anything today.</p>
			{:else}
				<ul class="preview__list">
					{#each preview as t (t.repo + ':' + t.tag)}
						<li>
							<span class="minus" aria-hidden="true">−</span>
							<span class="preview__what">
								<a href={refPath(t.repo, t.tag)}>{t.repo}:{t.tag}</a>
								<span class="digest">{shortDigest(t.digest)}</span>
							</span>
							<span class="muted nowrap"><Time unix={t.updatedAt} /></span>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	</div>
{/if}

{#if error}
	<ErrorState {error} retry={load} />
{:else if !policies}
	<Skeleton rows={3} columns={[2, 2, 3, 2, 1]} />
{:else if policies.length === 0}
	{#if !draft}
		<EmptyState
			icon="clock"
			title="No retention policies"
			description="Without a policy every tag stays forever. A common start: keep the newest 10 tags per repository."
		>
			<Button variant="primary" icon="plus" onclick={() => openEditor('new')}>New policy</Button>
		</EmptyState>
	{/if}
{:else}
	<Table label="Retention policies">
		<thead>
			<tr>
				<th>Name</th>
				<th>Scope</th>
				<th class="col-rule">Rule</th>
				<th class="col-protected">Protected</th>
				<th class="shrink"><span class="visually-hidden">Actions</span></th>
			</tr>
		</thead>
		<tbody>
			{#each policies as p (p.id)}
				<tr class:is-selected={editing === String(p.id)} class:is-off={!p.enabled}>
					<td>
						<span class="row">
							<a class="row-link" href="?edit={p.id}">{p.name}</a>
							{#if !p.enabled}<Badge>Off</Badge>{/if}
						</span>
					</td>
					<td class="mono scope"
						>{p.repoPattern}{#if p.tagPattern}<span class="subtle"> : {p.tagPattern}</span>{/if}</td
					>
					<td class="muted col-rule">{describePolicy(p)}</td>
					<td class="mono subtle col-protected">{p.keepPattern || 'none'}</td>
					<td class="shrink">
						<span class="row-actions">
							<Button variant="ghost" size="sm" onclick={() => toggle(p)}
								>{p.enabled ? 'Disable' : 'Enable'}</Button
							>
							<ConfirmButton
								label={`Delete policy ${p.name}`}
								iconOnly
								icon="trash"
								question="Delete policy?"
								onconfirm={() => remove(p)}
							/>
						</span>
					</td>
				</tr>
			{/each}
		</tbody>
	</Table>
{/if}

<style>
	.editor {
		display: grid;
		grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
		margin-bottom: var(--space-7);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
		overflow: hidden;
	}

	.editor__form {
		display: grid;
		gap: var(--space-4);
		align-content: start;
		padding: var(--space-5);
	}

	.grid2 {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-4);
	}

	.editor__preview {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
		padding: var(--space-5);
		border-left: 1px solid var(--border);
		background: var(--bg-sidebar);
	}

	.preview__head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--space-3);
	}

	.preview__head .muted {
		font-size: var(--text-sm);
	}

	.preview__none {
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.preview__list {
		max-height: 28rem;
		overflow: auto;
		margin: 0;
		padding: 0;
		list-style: none;
		font-size: var(--text-sm);
	}

	.preview__list li {
		display: grid;
		grid-template-columns: 1rem minmax(0, 1fr) auto;
		gap: var(--space-2);
		align-items: baseline;
		padding: 0.4375rem 0;
		border-bottom: 1px solid var(--border);
	}

	.preview__list li:last-child {
		border-bottom: 0;
	}

	.preview__what {
		overflow-wrap: anywhere;
	}

	.preview__what a {
		color: var(--fg);
	}

	.preview__what .digest {
		margin-left: var(--space-2);
	}

	.minus {
		color: var(--danger);
		font-weight: 600;
	}

	.scope {
		font-size: var(--text-sm);
	}

	tr.is-off td:not(:last-child) {
		opacity: 0.55;
	}

	@media (max-width: 64rem) {
		.editor {
			grid-template-columns: 1fr;
		}

		.editor__preview {
			border-left: 0;
			border-top: 1px solid var(--border);
		}
	}

	@media (max-width: 48rem) {
		.col-protected {
			display: none;
		}
	}

	@media (max-width: 40rem) {
		.grid2 {
			grid-template-columns: 1fr;
		}

		.col-rule {
			display: none;
		}

		.editor__form,
		.editor__preview {
			padding: var(--space-4);
		}
	}
</style>
