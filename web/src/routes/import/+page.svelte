<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '#lib/api.js';
	import { bytes, plural, when } from '#lib/format.js';
	import { importDone, parsePatterns } from '#lib/importForm.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Section from '#lib/components/Section.svelte';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Checkbox from '#lib/components/Checkbox.svelte';
	import Field from '#lib/components/Field.svelte';
	import Input from '#lib/components/Input.svelte';
	import Notice from '#lib/components/Notice.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Facts from '#lib/components/Facts.svelte';
	import Fact from '#lib/components/Fact.svelte';
	import Readout from '#lib/components/Readout.svelte';
	import type { ImportJob, ImportRepo } from '#lib/types.js';

	let job = $state<ImportJob | null>(null);
	let loaded = $state(false);
	let error = $state<unknown>(null);
	let busy = $state<'' | 'preview' | 'start' | 'cancel'>('');

	let url = $state('');
	let username = $state('');
	let password = $state('');
	let insecure = $state(false);
	let repositories = $state('');
	let preview = $state<ImportRepo[] | null>(null);
	let formError = $state('');

	const running = $derived(job?.status === 'running');
	const previewTags = $derived(preview?.reduce((n, r) => n + r.tags.length, 0) ?? 0);

	let timer: ReturnType<typeof setTimeout> | undefined;

	async function load() {
		error = null;
		clearTimeout(timer);
		try {
			const res = await api.get<{ job: ImportJob | null }>('/api/import');
			const wasRunning = job?.status === 'running';
			job = res.job;
			loaded = true;
			if (job?.status === 'running') timer = setTimeout(load, 1000);
			else if (wasRunning && job) finished(job);
		} catch (e) {
			error = e;
		}
	}
	load();
	onDestroy(() => clearTimeout(timer));

	function finished(j: ImportJob) {
		const p = j.progress;
		if (j.status === 'failed') notify(j.error ?? 'Import failed', 'danger');
		else if (j.status === 'canceled') notify('Import canceled.');
		else if (p.tagsFailed) notify(`Import done, ${plural(p.tagsFailed, 'tag')} failed.`, 'danger');
		else notify(`Import done: ${plural(p.tagsCopied, 'tag')} copied.`);
	}

	function source() {
		return { url, username, password, insecure, repositories: parsePatterns(repositories) };
	}

	async function runPreview() {
		busy = 'preview';
		formError = '';
		try {
			const res = await api.post<{ repositories: ImportRepo[] }>('/api/import/preview', source());
			preview = res.repositories;
		} catch (e) {
			preview = null;
			formError = e instanceof Error ? e.message : String(e);
		} finally {
			busy = '';
		}
	}

	async function start(e: SubmitEvent) {
		e.preventDefault();
		busy = 'start';
		formError = '';
		try {
			const res = await api.post<{ job: ImportJob }>('/api/import', source());
			job = res.job;
			password = '';
			preview = null;
			timer = setTimeout(load, 1000);
		} catch (err) {
			formError = err instanceof Error ? err.message : String(err);
		} finally {
			busy = '';
		}
	}

	async function cancel() {
		busy = 'cancel';
		try {
			await api.post('/api/import/cancel');
			await load();
		} catch (e) {
			fail(e);
		} finally {
			busy = '';
		}
	}

	const statusBadge = {
		running: { tone: 'accent', label: 'Running' },
		done: { tone: 'success', label: 'Done' },
		failed: { tone: 'danger', label: 'Failed' },
		canceled: { tone: 'neutral', label: 'Canceled' }
	} as const;
</script>

<svelte:head><title>Import · doreg</title></svelte:head>

<PageHeader
	title="Import"
	description="Copy repositories from another registry, such as registry:2. Digests stay the same, and tags that are already up to date are skipped, so an import can simply be run again."
/>

{#if error}
	<ErrorState {error} retry={load} />
{:else if !loaded}
	<Skeleton rows={3} columns={[2, 1, 1]} />
{:else}
	{#if job}
		{@const p = job.progress}
		{@const badge = statusBadge[job.status]}
		<section class="report block" aria-live="polite">
			<header class="report__head">
				<div class="report__title">
					<h2>{running ? 'Importing' : 'Last import'} from {job.source}</h2>
					<p class="muted">
						Started {when(job.startedAt)} by {job.actor}.
						{#if job.finishedAt}Finished {when(job.finishedAt)}.{/if}
					</p>
				</div>
				<div class="row">
					<Badge tone={badge.tone}>{badge.label}</Badge>
					{#if running}
						<Button size="sm" variant="danger" loading={busy === 'cancel'} onclick={cancel}
							>Cancel</Button
						>
					{/if}
				</div>
			</header>
			{#if running}
				<span
					class="bar"
					role="progressbar"
					aria-label="Tags handled"
					aria-valuemin={0}
					aria-valuemax={100}
					aria-valuenow={Math.round(importDone(p) * 100)}
				>
					<span class="bar__fill" style:width="{importDone(p) * 100}%"></span>
				</span>
			{/if}
			<Facts label="Import results">
				<Fact label="Transferred" big><Readout value={bytes(p.bytes)} /></Fact>
				<Fact label="Repositories" measure>{p.repositories}</Fact>
				<Fact label="Tags copied" measure>{p.tagsCopied} / {p.tags}</Fact>
				<Fact label="Already up to date" measure>{p.tagsSkipped}</Fact>
				<Fact label="Failed" measure>{p.tagsFailed}</Fact>
				<Fact label="Layers" measure>{p.blobs}</Fact>
			</Facts>
			{#if job.error}<Notice tone="danger">{job.error}</Notice>{/if}
			{#if p.errors.length}
				<ul class="errors">
					{#each p.errors as e, i (i)}<li>{e}</li>{/each}
				</ul>
				{#if p.tagsFailed > p.errors.length}
					<p class="subtle">Only the first {p.errors.length} failures are listed.</p>
				{/if}
			{/if}
		</section>
	{/if}

	<form class="form" onsubmit={start}>
		<Section
			title="Source registry"
			description="The password is used for this import only and is not stored."
		>
			<div class="fields">
				<Field label="Registry URL" hint="e.g. https://old-registry.example.com:5000">
					<Input bind:value={url} required placeholder="https://" autocomplete="off" />
				</Field>
				<div class="pair">
					<Field label="Username" hint="Leave empty if the registry needs no login.">
						<Input bind:value={username} autocomplete="off" />
					</Field>
					<Field label="Password or token">
						<Input type="password" bind:value={password} autocomplete="new-password" />
					</Field>
				</div>
				<Field
					label="Repositories"
					hint="Names or patterns like team/*, separated by commas. Empty imports everything. Registries without a catalog, such as Docker Hub, need plain names."
				>
					<Input bind:value={repositories} mono placeholder="all repositories" />
				</Field>
				<Checkbox
					label="Skip TLS certificate check"
					description="Only for a source with a self-signed certificate."
					bind:checked={insecure}
				/>
				{#if formError}<Notice tone="danger">{formError}</Notice>{/if}
				<Notice>
					Imported pushes are recorded in the audit log but trigger no webhooks. Repositories
					without a slash land in the project library.
				</Notice>
				<div class="row">
					<Button
						disabled={!url || !!busy || running}
						loading={busy === 'preview'}
						onclick={runPreview}>{busy === 'preview' ? 'Connecting…' : 'Preview'}</Button
					>
					<Button
						type="submit"
						variant="primary"
						disabled={!url || !!busy || running}
						loading={busy === 'start'}>Start import</Button
					>
				</div>
			</div>
		</Section>
	</form>

	{#if preview}
		<Section
			title="Found in the source"
			description="{plural(preview.length, 'repository', 'repositories')}, {plural(
				previewTags,
				'tag'
			)}"
		>
			{#if preview.length === 0}
				<p class="muted">No repositories with tags match.</p>
			{:else}
				<Table label="Repositories to import" maxHeight="24rem">
					<thead>
						<tr><th>Repository</th><th class="right">Tags</th></tr>
					</thead>
					<tbody>
						{#each preview as r (r.name)}
							<tr>
								<td class="mono">{r.name}</td>
								<td class="right" title={r.tags.slice(0, 20).join(', ')}>{r.tags.length}</td>
							</tr>
						{/each}
					</tbody>
				</Table>
			{/if}
		</Section>
	{/if}
{/if}

<style>
	.block {
		margin-bottom: var(--space-7);
	}

	.report {
		display: grid;
		gap: var(--space-4);
		padding: var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-xl);
		background: var(--surface);
		box-shadow: var(--shadow-control);
	}

	.report :global(.facts) {
		background: var(--bg-sidebar);
		box-shadow: none;
	}

	.report__head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-3);
	}

	.report__title {
		display: grid;
		gap: 0.125rem;
		min-width: 0;
	}

	.report__title h2 {
		overflow-wrap: anywhere;
	}

	.report__title p {
		font-size: var(--text-sm);
	}

	.bar {
		display: block;
		height: 0.375rem;
		border-radius: 999px;
		background: var(--surface-raised);
		overflow: hidden;
	}

	.bar__fill {
		display: block;
		height: 100%;
		background: var(--accent);
		transition: width var(--dur-micro) var(--ease);
	}

	.errors {
		max-height: 16rem;
		overflow: auto;
		margin: 0;
		padding: 0;
		list-style: none;
		border-top: 1px solid var(--border);
		font-size: var(--text-sm);
		color: var(--danger);
	}

	.errors li {
		padding: var(--space-2) 0;
		border-bottom: 1px solid var(--border);
		overflow-wrap: anywhere;
	}

	.form {
		max-width: 40rem;
		margin-bottom: var(--space-7);
	}

	.fields {
		display: grid;
		gap: var(--space-4);
		padding: var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-xl);
		background: var(--surface);
	}

	.pair {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-4);
	}

	@media (max-width: 40rem) {
		.pair {
			grid-template-columns: 1fr;
		}

		.report {
			padding: var(--space-4);
		}
	}
</style>
