<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import { bytes, refPath, shortDigest, when } from '#lib/format.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import Section from '#lib/components/Section.svelte';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Checkbox from '#lib/components/Checkbox.svelte';
	import Field from '#lib/components/Field.svelte';
	import Input from '#lib/components/Input.svelte';
	import Notice from '#lib/components/Notice.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import Facts from '#lib/components/Facts.svelte';
	import Fact from '#lib/components/Fact.svelte';
	import type { GcRun, GcSettings } from '#lib/types.js';

	interface Status {
		settings: GcSettings;
		running: boolean;
		nextRun: number;
		runs: GcRun[];
	}

	let status = $state<Status | null>(null);
	let error = $state<unknown>(null);
	let settings = $state<GcSettings | null>(null);
	let detail = $state<GcRun | null>(null);
	let detailError = $state<unknown>(null);
	let busy = $state<'' | 'preview' | 'run' | 'save'>('');
	let applyRetention = $state(true);

	const tab = $derived(page.url.searchParams.get('tab') === 'settings' ? 'settings' : 'runs');
	const runId = $derived(Number(page.url.searchParams.get('run') ?? 0));

	async function load() {
		error = null;
		try {
			status = await api.get<Status>('/api/gc');
			settings ??= { ...status.settings };
		} catch (e) {
			error = e;
		}
	}
	load();

	function loadDetail(id: number) {
		detailError = null;
		api.get<GcRun>(`/api/gc/runs/${id}`).then(
			(r) => (detail = r),
			(e) => (detailError = e)
		);
	}

	$effect(() => {
		const id = runId;
		if (!id) {
			detail = null;
			return;
		}
		if (detail?.id === id) return;
		loadDetail(id);
	});

	async function run(dryRun: boolean) {
		busy = dryRun ? 'preview' : 'run';
		try {
			const r = await api.post<GcRun>('/api/gc/run', { dryRun, applyRetention });
			detail = r;
			await goto(`?run=${r.id}`, { reset: false });
			if (r.status === 'failed') notify(r.error ?? 'Run failed', 'danger');
			else if (!dryRun) notify(`Freed ${bytes(r.freedBytes)}.`);
			await load();
		} catch (e) {
			fail(e);
		} finally {
			busy = '';
		}
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!settings) return;
		busy = 'save';
		try {
			await api.put('/api/gc/settings', settings);
			notify('Settings saved.');
			await load();
		} catch (err) {
			fail(err);
		} finally {
			busy = '';
		}
	}

	const report = $derived(detail?.report);
	const nothing = $derived(
		report &&
			report.deletedTags.length === 0 &&
			report.deletedManifests.length === 0 &&
			report.deletedBlobs === 0 &&
			report.orphanFiles === 0 &&
			report.staleUploads === 0
	);
	const triggerLabel: Record<string, string> = {
		schedule: 'Scheduled',
		manual: 'Manual',
		preview: 'Preview',
		cli: 'CLI'
	};
</script>

<svelte:head><title>Garbage collection · doreg</title></svelte:head>

<PageHeader title="Garbage collection">
	{#snippet description()}
		Removes unreferenced manifests and layers while the registry keeps serving.
		{#if status?.nextRun}Next scheduled run <Time
				unix={status.nextRun}
			/>.{:else if status}Scheduled runs are off.{/if}
	{/snippet}
	{#snippet actions()}
		<Button
			disabled={!!busy || status?.running}
			loading={busy === 'preview'}
			onclick={() => run(true)}>{busy === 'preview' ? 'Checking…' : 'Preview'}</Button
		>
		<Button
			variant="primary"
			disabled={!!busy || status?.running}
			loading={busy === 'run'}
			onclick={() => run(false)}>{busy === 'run' ? 'Collecting…' : 'Run now'}</Button
		>
	{/snippet}
</PageHeader>

<Tabs
	label="Garbage collection sections"
	active={tab}
	items={[
		{ key: 'runs', label: 'Runs', href: '?tab=runs', count: status?.runs.length },
		{ key: 'settings', label: 'Settings', href: '?tab=settings' }
	]}
/>

{#if error}
	<ErrorState {error} retry={load} />
{:else if !status || !settings}
	<Skeleton rows={4} columns={[2, 2, 1, 1, 1]} />
{:else if tab === 'runs'}
	<div class="run-opts">
		<Checkbox
			label="Apply retention policies"
			description="Preview and Run now also delete tags matched by enabled retention policies."
			bind:checked={applyRetention}
		/>
		{#if status.running}<Badge tone="accent">A run is in progress</Badge>{/if}
	</div>

	{#if detailError}
		<div class="block"><ErrorState error={detailError} retry={() => loadDetail(runId)} /></div>
	{:else if runId && !detail}
		<div class="block"><Skeleton rows={3} columns={[3, 1, 1]} /></div>
	{:else if detail && report}
		<section class="report block ticks" aria-live="polite">
			<header class="report__head">
				<div class="report__title">
					<h2>
						{triggerLabel[detail.trigger] ?? detail.trigger} run {detail.id}{detail.dryRun
							? ', preview'
							: ''}
					</h2>
					<p class="muted">
						Started {when(detail.startedAt)}.
						{#if detail.dryRun}Nothing was deleted.{/if}
					</p>
				</div>
				<div class="row">
					{#if detail.status === 'failed'}<Badge tone="danger">Failed</Badge>{/if}
					<Button variant="ghost" size="sm" icon="x" label="Close report" href="?tab=runs" />
				</div>
			</header>
			<Facts label="Run results">
				<Fact label={detail.dryRun ? 'Would free' : 'Freed'} measure
					>{bytes(detail.freedBytes)}</Fact
				>
				<Fact label="Tags by retention" measure>{report.deletedTags.length}</Fact>
				<Fact label="Manifests" measure>{report.deletedManifests.length}</Fact>
				<Fact label="Blobs" measure>{report.deletedBlobs + report.orphanFiles}</Fact>
				{#if report.orphanFiles}<Fact label="Orphan files" measure>{report.orphanFiles}</Fact>{/if}
				<Fact label="Stale uploads" measure>{report.staleUploads}</Fact>
				{#if report.prunedAuditEntries}<Fact label="Audit entries pruned" measure
						>{report.prunedAuditEntries}</Fact
					>{/if}
				{#if report.keptRecent}<Fact label="Untagged kept" measure>{report.keptRecent}</Fact>{/if}
			</Facts>

			{#if detail.error}<Notice tone="danger">{detail.error}</Notice>{/if}

			{#if nothing}
				<p class="report__nothing">
					Nothing to collect. Every manifest is reachable and every layer is in use.
				</p>
			{:else if report.deletedTags.length || report.deletedManifests.length}
				<ul class="removed">
					{#each report.deletedTags as t (t.repo + ':' + t.tag)}
						<li>
							<span class="removed__mark" aria-hidden="true">−</span>
							<span class="removed__what">
								{#if detail.dryRun}<a href={refPath(t.repo, t.tag)}>{t.repo}:{t.tag}</a
									>{:else}{t.repo}:{t.tag}{/if}
								<span class="digest">{shortDigest(t.digest)}</span>
							</span>
							<span class="removed__why">policy “{t.policy}”</span>
							<span class="removed__age"><Time unix={t.updatedAt} /></span>
						</li>
					{/each}
					{#each report.deletedManifests as d (d.repo + d.digest)}
						<li>
							<span class="removed__mark" aria-hidden="true">−</span>
							<span class="removed__what"
								>{d.repo}<span class="digest">@{shortDigest(d.digest)}</span></span
							>
							<span class="removed__why">untagged</span>
							<span class="removed__age"><Time unix={d.createdAt} /></span>
						</li>
					{/each}
				</ul>
				{#if report.deletedRepos.length}
					<p class="subtle report__foot">
						Empty repositories removed: {report.deletedRepos.join(', ')}
					</p>
				{/if}
			{/if}
		</section>
	{/if}

	<Section title="History">
		{#if status.runs.length === 0}
			<EmptyState
				icon="recycle"
				title="No runs yet"
				description="Start with a preview to see what would be removed."
			/>
		{:else}
			<Table label="Run history">
				<thead>
					<tr>
						<th>Run</th>
						<th>Started</th>
						<th class="right col-manifests">Manifests</th>
						<th class="right">Freed</th>
						<th>Status</th>
					</tr>
				</thead>
				<tbody>
					{#each status.runs as r (r.id)}
						<tr class:is-selected={r.id === runId}>
							<td>
								<a
									class="row-link"
									href="?run={r.id}"
									aria-current={r.id === runId ? 'true' : undefined}
									>#{r.id}
									<span class="muted trigger">{triggerLabel[r.trigger] ?? r.trigger}</span></a
								>
							</td>
							<td class="muted nowrap"><Time unix={r.startedAt} /></td>
							<td class="right muted col-manifests">{r.deletedManifests}</td>
							<td
								class="right nowrap"
								class:muted={r.dryRun}
								title={r.dryRun ? 'Would free' : 'Freed'}>{bytes(r.freedBytes)}</td
							>
							<td>
								{#if r.status === 'failed'}<Badge tone="danger" title={r.error}>Failed</Badge>
								{:else if r.status === 'running'}<Badge tone="accent">Running</Badge>
								{:else if r.dryRun}<Badge>Preview</Badge>
								{:else}<Badge tone="success">Done</Badge>{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</Table>
		{/if}
	</Section>
{:else}
	<form class="settings" onsubmit={save}>
		<Section title="Schedule">
			<div class="fields">
				<Field label="Run every (hours)" hint="0 turns scheduled runs off.">
					<Input type="number" min={0} bind:value={settings.intervalHours} />
				</Field>
				<Checkbox
					label="Scheduled runs apply retention policies"
					bind:checked={settings.applyRetention}
				/>
			</div>
		</Section>
		<Section title="Untagged manifests">
			<div class="fields">
				<Checkbox
					label="Delete untagged manifests"
					description="Manifests no tag points to, e.g. after a tag moved to a new build."
					bind:checked={settings.deleteUntagged}
				/>
				<Field
					label="Keep untagged manifests pulled within (days)"
					hint="Protects deployments pinned to a digest after the tag moved on."
				>
					<Input
						type="number"
						min={0}
						disabled={!settings.deleteUntagged}
						bind:value={settings.keepPulledUntaggedDays}
					/>
				</Field>
			</div>
		</Section>
		<Section title="Safety">
			<div class="fields">
				<Field
					label="Grace period (minutes)"
					hint="Fresh uploads are never collected, so pushes in progress stay safe."
				>
					<Input type="number" min={10} bind:value={settings.graceMinutes} />
				</Field>
				<Field label="Abandon unfinished uploads after (hours)">
					<Input type="number" min={1} bind:value={settings.uploadMaxAgeHours} />
				</Field>
			</div>
		</Section>
		<Section title="Audit log">
			<div class="fields">
				<Field
					label="Keep audit entries for (days)"
					hint="Older entries are pruned during garbage collection. 0 keeps them forever."
				>
					<Input type="number" min={0} bind:value={settings.auditRetentionDays} />
				</Field>
			</div>
		</Section>
		<div class="settings__actions">
			<Button variant="primary" type="submit" loading={busy === 'save'}>Save settings</Button>
		</div>
	</form>
{/if}

<style>
	.run-opts {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-3);
		flex-wrap: wrap;
		margin-bottom: var(--space-5);
	}

	.block {
		margin-bottom: var(--space-7);
	}

	.report {
		display: grid;
		gap: var(--space-4);
		padding: var(--space-5);
		background: var(--surface);
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
		text-wrap: balance;
		overflow-wrap: break-word;
		min-width: 0;
	}

	.report__title p {
		font-size: var(--text-sm);
	}

	.report__nothing {
		color: var(--fg-muted);
	}

	.removed {
		max-height: 24rem;
		overflow: auto;
		margin: 0;
		padding: 0;
		list-style: none;
		border-top: 1px solid var(--border);
	}

	.removed li {
		display: grid;
		grid-template-columns: 1rem minmax(0, 1fr) auto auto;
		gap: var(--space-3);
		align-items: baseline;
		padding: var(--space-2) 0;
		border-bottom: 1px solid var(--border);
		font-size: var(--text-sm);
	}

	.removed__mark {
		color: var(--danger);
		font-weight: 600;
	}

	.removed__what {
		overflow-wrap: anywhere;
	}

	.removed__what .digest {
		margin-left: var(--space-2);
	}

	.removed__why,
	.removed__age {
		color: var(--fg-muted);
		white-space: nowrap;
	}

	.report__foot {
		font-size: var(--text-sm);
	}

	.settings {
		max-width: 36rem;
	}

	.fields {
		display: grid;
		gap: var(--space-4);
		padding: var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
	}

	.fields :global(.input) {
		max-width: 10rem;
	}

	.settings :global(.section + .section) {
		margin-top: var(--space-6);
	}

	.settings__actions {
		margin-top: var(--space-5);
	}

	@media (max-width: 40rem) {
		.col-manifests,
		.trigger {
			display: none;
		}

		.removed li {
			grid-template-columns: 1rem minmax(0, 1fr) auto;
		}

		.removed__why {
			display: none;
		}

		.report {
			padding: var(--space-4);
		}
	}
</style>
