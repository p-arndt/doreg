<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, q } from '#lib/api.js';
	import {
		bytes,
		fraction,
		healthcheckCommand,
		healthcheckTiming,
		historyCommand,
		mediaTypeLabel,
		platform,
		plural,
		projectOf,
		pullText,
		refPath,
		shortDigest
	} from '#lib/format.js';
	import { hasProjectRole, repoCrumbs } from '#lib/session.svelte.js';
	import { prefs } from '#lib/prefs.svelte.js';
	import { loadProjectRole } from '#lib/projectRole.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import Section from '#lib/components/Section.svelte';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import PullReference from '#lib/components/PullReference.svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import ConfirmButton from '#lib/components/ConfirmButton.svelte';
	import DescriptionList from '#lib/components/DescriptionList.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import Facts from '#lib/components/Facts.svelte';
	import Fact from '#lib/components/Fact.svelte';
	import Readout from '#lib/components/Readout.svelte';
	import type { ManifestDetail, ProjectRole } from '#lib/types.js';

	let { repo, ref }: { repo: string; ref: string } = $props();
	let m = $state<ManifestDetail | null>(null);
	let error = $state<unknown>(null);

	const host = location.host;
	const isDigest = $derived(ref.includes(':'));
	const project = $derived(projectOf(repo));
	let role = $state<ProjectRole>('');
	$effect(() => {
		loadProjectRole(project).then((r) => (role = r));
	});
	const canDelete = $derived(hasProjectRole(role, 'write'));
	const pullRef = $derived(`${host}/${repo}${isDigest ? '@' : ':'}${ref}`);

	async function load() {
		error = null;
		try {
			m = await api.get<ManifestDetail>(`/api/manifest?repo=${q(repo)}&ref=${q(ref)}`);
		} catch (e) {
			error = e;
		}
	}
	load();

	const isIndex = $derived(!!m?.children);
	const images = $derived(m?.children?.filter((c) => !c.attestation) ?? []);
	const attestations = $derived(m?.children?.filter((c) => c.attestation) ?? []);
	const layers = $derived(m?.layers ?? []);
	const totalLayers = $derived(layers.reduce((sum, l) => sum + l.size, 0));
	// The largest layer leads the strip's caption until the pointer picks another.
	let active = $state(0);
	$effect(() => {
		let biggest = 0;
		layers.forEach((l, i) => {
			if (l.size > layers[biggest].size) biggest = i;
		});
		active = biggest;
	});
	const cfg = $derived(m?.config?.config);
	const builtAt = $derived(
		m?.summary.created ? Math.floor(Date.parse(m.summary.created) / 1000) : 0
	);

	// Non-empty history entries map one-to-one onto layers, in order.
	const history = $derived.by(() => {
		let layer = 0;
		return (m?.config?.history ?? []).map((h) => ({
			...h,
			command: historyCommand(h.created_by),
			layer: h.empty_layer ? undefined : layers[layer++]
		}));
	});

	const layerCommands = $derived(history.filter((h) => h.layer).map((h) => h.command));

	const configItems = $derived.by((): [string, string][] => {
		if (!m?.config) return [];
		const items: [string, string][] = [['Platform', platform(m.config)]];
		if (cfg?.Entrypoint) items.push(['Entrypoint', JSON.stringify(cfg.Entrypoint)]);
		if (cfg?.Cmd) items.push(['Command', JSON.stringify(cfg.Cmd)]);
		if (cfg?.WorkingDir) items.push(['Working dir', cfg.WorkingDir]);
		if (cfg?.User) items.push(['User', cfg.User]);
		if (cfg?.ExposedPorts) items.push(['Ports', Object.keys(cfg.ExposedPorts).join(', ')]);
		if (cfg?.Volumes) items.push(['Volumes', Object.keys(cfg.Volumes).join(', ')]);
		if (cfg?.StopSignal) items.push(['Stop signal', cfg.StopSignal]);
		const check = healthcheckCommand(cfg?.Healthcheck?.Test);
		if (check) {
			items.push(['Healthcheck', check]);
			const timing = check === 'disabled' ? '' : healthcheckTiming(cfg!.Healthcheck!);
			if (timing) items.push(['Health timing', timing]);
		}
		if (cfg?.Shell) items.push(['Shell', JSON.stringify(cfg.Shell)]);
		if (cfg?.OnBuild?.length) items.push(['On build', cfg.OnBuild.join(' · ')]);
		if (m.config.author) items.push(['Author', m.config.author]);
		return items;
	});
	const env = $derived(
		(cfg?.Env ?? []).map((e): [string, string] => {
			const i = e.indexOf('=');
			return i < 0 ? [e, ''] : [e.slice(0, i), e.slice(i + 1)];
		})
	);
	const labels = $derived(Object.entries(cfg?.Labels ?? {}));

	function attestationTarget(digest: string): string {
		const raw = m?.raw as {
			manifests?: { digest: string; annotations?: Record<string, string> }[];
		};
		return (
			raw?.manifests?.find((x) => x.digest === digest)?.annotations?.[
				'vnd.docker.reference.digest'
			] ?? ''
		);
	}

	const tabs = $derived.by(() => {
		if (!m) return [];
		const t = isIndex
			? [{ key: 'platforms', label: 'Platforms', href: '?tab=platforms', count: images.length }]
			: [
					{ key: 'layers', label: 'Layers', href: '?tab=layers', count: layers.length },
					...(m.config || m.artifactType
						? [{ key: 'config', label: 'Configuration', href: '?tab=config' }]
						: []),
					...(history.length
						? [{ key: 'history', label: 'History', href: '?tab=history', count: history.length }]
						: [])
				];
		if (m.referrers.length)
			t.push({
				key: 'referrers',
				label: 'Attached',
				href: '?tab=referrers',
				count: m.referrers.length
			});
		t.push({ key: 'raw', label: 'Raw manifest', href: '?tab=raw' });
		return t;
	});
	const tab = $derived.by(() => {
		const want = page.url.searchParams.get('tab');
		return tabs.find((t) => t.key === want)?.key ?? tabs[0]?.key ?? '';
	});

	async function remove() {
		if (!m) return;
		try {
			await api.del(`/api/manifest?repo=${q(repo)}&digest=${q(m.digest)}`);
			notify('Manifest deleted. Garbage collection frees the layers.');
			await goto(`/r/${repo}`);
		} catch (e) {
			fail(e);
		}
	}

	const crumbs = $derived([
		...repoCrumbs(project),
		{ href: `/r/${repo}`, label: repo.slice(repo.indexOf('/') + 1) },
		...(m && m.parents.length > 0
			? [{ href: refPath(repo, m.parents[0]), label: `index ${shortDigest(m.parents[0])}` }]
			: [])
	]);
</script>

<svelte:head
	><title>{repo}{isDigest ? '@' + shortDigest(ref) : ':' + ref} · doreg</title></svelte:head
>

<PageHeader title={isDigest ? shortDigest(ref) : ref} {crumbs}>
	{#snippet meta()}
		{#if m}
			<span class="digest-line">
				<span class="digest" title={m.digest}>{m.digest}</span>
				<CopyButton value={m.digest} label="Copy digest" />
			</span>
		{/if}
	{/snippet}
	{#snippet actions()}
		{#if m && canDelete}
			<ConfirmButton
				label="Delete"
				icon="trash"
				size="md"
				question={m.tags.length ? `Also removes ${plural(m.tags.length, 'tag')}.` : 'Delete?'}
				onconfirm={remove}
			/>
		{/if}
	{/snippet}
</PageHeader>

{#if error}
	<ErrorState {error} retry={load} />
{:else if !m}
	<Skeleton rows={6} columns={[1, 4, 3, 1]} />
{:else}
	<div class="summary">
		<PullReference reference={pullRef} />
		<Facts label="Manifest">
			<Fact label="Type">{mediaTypeLabel(m.mediaType)}</Fact>
			<Fact label={isIndex ? 'Size, all platforms' : 'Compressed size'} big
				><Readout value={bytes(m.summary.size)} /></Fact
			>
			{#if builtAt}<Fact label="Built"><Time unix={builtAt} /></Fact>{/if}
			<Fact label="Pushed"><Time unix={m.createdAt} /></Fact>
			<Fact label="Last pulled"><Time unix={m.lastPulledAt} /></Fact>
			{#if m.tags.length}
				<Fact label={m.tags.length === 1 ? 'Tag' : 'Tags'}>
					{#each m.tags as t (t)}<a class="tag-link" href={refPath(repo, t)}><Badge>{t}</Badge></a
						>{/each}
				</Fact>
			{:else if m.parents.length === 0}
				<Fact label="Tags"><Badge tone="warning">Untagged</Badge></Fact>
			{/if}
			{#if m.subject}
				<Fact label="Describes" measure
					><a href={refPath(repo, m.subject)}>{shortDigest(m.subject)}</a></Fact
				>
			{/if}
		</Facts>
	</div>

	<Tabs label="Manifest sections" items={tabs} active={tab} />

	{#if tab === 'platforms'}
		<Section title="Images" description="One image per platform. Docker picks the matching one.">
			<Table label="Platforms">
				<thead>
					<tr>
						<th>Platform</th>
						<th class="col-digest">Digest</th>
						<th class="right">Size</th>
						<th class="shrink"><span class="visually-hidden">Actions</span></th>
					</tr>
				</thead>
				<tbody>
					{#each images as c (c.digest)}
						<tr>
							<td><a class="row-link" href={refPath(repo, c.digest)}>{platform(c.platform)}</a></td>
							<td class="col-digest"
								><span class="digest" title={c.digest}>{shortDigest(c.digest)}</span></td
							>
							<td class="right muted nowrap">{bytes(c.imageSize)}</td>
							<td class="shrink">
								<span class="row-actions">
									<CopyButton
										value={prefs.copyMode === 'ref'
											? `${host}/${repo}@${c.digest}`
											: pullText(pullRef, 'command', platform(c.platform))}
										label={prefs.copyMode === 'ref'
											? `Copy reference of the ${platform(c.platform)} image`
											: `Copy docker pull --platform ${platform(c.platform)}`}
									/>
								</span>
							</td>
						</tr>
					{/each}
				</tbody>
			</Table>
		</Section>
		{#if attestations.length}
			<Section
				title="Attestations"
				description="SBOM and provenance attached by buildx. Not runnable images."
			>
				<Table label="Attestations">
					<thead>
						<tr><th>Digest</th><th>Describes</th><th class="right">Size</th></tr>
					</thead>
					<tbody>
						{#each attestations as c (c.digest)}
							{@const target = attestationTarget(c.digest)}
							<tr>
								<td>
									<a class="row-link mono" href={refPath(repo, c.digest)}>{shortDigest(c.digest)}</a
									>
								</td>
								<td>
									{#if target}
										{@const img = images.find((i) => i.digest === target)}
										<a class="muted" href={refPath(repo, target)}
											>{img ? platform(img.platform) : shortDigest(target)}</a
										>
									{/if}
								</td>
								<td class="right muted nowrap">{bytes(c.imageSize)}</td>
							</tr>
						{/each}
					</tbody>
				</Table>
			</Section>
		{/if}
	{:else if tab === 'layers'}
		{#if layers.length === 0}
			<EmptyState
				icon="layers"
				title="No layers"
				description="This manifest references no blobs."
			/>
		{:else}
			<figure class="strip" aria-label="Layers drawn to scale">
				<div class="strip__bar">
					{#each layers as l, i (l.digest + i)}
						<button
							type="button"
							class="strip__seg"
							class:is-on={active === i}
							style:flex-grow={Math.max(l.size, 1)}
							aria-label={`Layer ${i + 1}, ${bytes(l.size)}`}
							onmouseenter={() => (active = i)}
							onfocus={() => (active = i)}
							onclick={() => (active = i)}
						></button>
					{/each}
				</div>
				<figcaption class="strip__caption">
					{#if layers[active]}
						{@const l = layers[active]}
						<span class="strip__which">Layer {active + 1} of {layers.length}</span>
						<span class="measure">{bytes(l.size)}</span>
						<span class="measure muted">{Math.round(fraction(l.size, totalLayers) * 100)}%</span>
						{#if layerCommands[active]}<code class="strip__cmd">{layerCommands[active]}</code>{/if}
					{/if}
				</figcaption>
			</figure>
			<Table label="Layers">
				<thead>
					<tr>
						<th class="shrink">Layer</th>
						<th>Digest</th>
						<th class="col-cmd">Created by</th>
						<th class="right">Share</th>
						<th class="right">Size</th>
					</tr>
				</thead>
				<tbody>
					{#each layers as l, i (l.digest + i)}
						<tr class:is-selected={active === i} onmouseenter={() => (active = i)}>
							<td class="shrink subtle measure">{i + 1}</td>
							<td>
								<span class="row">
									<span class="mono layer-digest" title={`${l.digest}\n${l.mediaType}`}
										>{shortDigest(l.digest)}</span
									>
									{#if l.external}<Badge>external</Badge>{/if}
								</span>
							</td>
							<td class="col-cmd"><span class="cmd-cell">{layerCommands[i] ?? ''}</span></td>
							<td class="right muted nowrap">{Math.round(fraction(l.size, totalLayers) * 100)}%</td>
							<td class="right nowrap">{bytes(l.size)}</td>
						</tr>
					{/each}
				</tbody>
			</Table>
		{/if}
	{:else if tab === 'config'}
		{#if m.config}
			<Section title="Runtime">
				<DescriptionList items={configItems} mono />
			</Section>
			{#if env.length}
				<Section title="Environment">
					<DescriptionList items={env} mono monoKeys />
				</Section>
			{/if}
			{#if labels.length}
				<Section title="Labels">
					<DescriptionList items={labels} mono monoKeys />
				</Section>
			{/if}
		{:else if m.artifactType}
			<Section title="Artifact">
				<DescriptionList items={[['Type', m.artifactType]]} mono />
			</Section>
		{/if}
	{:else if tab === 'history'}
		<ol class="history">
			{#each history as h, i (i)}
				<li class:history--empty={!h.layer}>
					<span class="history__step num">{i + 1}</span>
					<div class="history__body">
						<code class="history__cmd">{h.command || '(no command recorded)'}</code>
						{#if h.comment}<span class="subtle">{h.comment}</span>{/if}
					</div>
					<span class="history__size nowrap">
						{#if h.layer}{bytes(h.layer.size)}{:else}<span class="subtle">no layer</span>{/if}
					</span>
				</li>
			{/each}
		</ol>
	{:else if tab === 'referrers'}
		<Table label="Attached artifacts">
			<thead><tr><th>Digest</th><th>Type</th><th>Pushed</th></tr></thead>
			<tbody>
				{#each m.referrers as r (r.digest)}
					<tr>
						<td
							><a class="row-link mono" href={refPath(repo, r.digest)}>{shortDigest(r.digest)}</a
							></td
						>
						<td class="mono muted">{r.artifactType ?? ''}</td>
						<td class="muted nowrap"><Time unix={r.createdAt} /></td>
					</tr>
				{/each}
			</tbody>
		</Table>
	{:else if tab === 'raw'}
		<div class="raw">
			<div class="raw__head">
				<span class="subtle">{m.mediaType}</span>
				<span class="subtle measure">{bytes(m.manifestSize)}</span>
				<CopyButton value={JSON.stringify(m.raw, null, 2)} text="Copy JSON" />
			</div>
			<pre>{JSON.stringify(m.raw, null, 2)}</pre>
		</div>
	{/if}
{/if}

<style>
	.digest-line {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		min-width: 0;
		max-width: 100%;
	}

	.digest-line .digest {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.summary {
		display: grid;
		gap: var(--space-3);
		margin: calc(-1 * var(--space-2)) 0 var(--space-6);
	}

	.summary > :global(.pull) {
		max-width: 36rem;
	}

	.tag-link:hover {
		text-decoration: none;
	}

	.layer-digest {
		color: var(--fg);
		font-size: var(--text-sm);
	}

	/* The signature: the image drawn as its layers, each segment as wide as its share of the bytes. */
	.strip {
		margin: 0 0 var(--space-5);
	}

	.strip__bar {
		display: flex;
		gap: var(--space-1);
		height: 0.625rem;
	}

	.strip__seg {
		flex-basis: 0;
		min-width: 6px;
		padding: 0;
		border: 0;
		border-radius: var(--radius-xs);
		background: var(--track-strong);
		cursor: pointer;
		transition: background-color var(--dur-micro) var(--ease);
	}

	.strip__seg:nth-child(even) {
		background: var(--border-hover);
	}

	.strip__seg:hover {
		background: var(--fg-subtle);
	}

	/* The chosen layer is the one lime mark in the strip. */
	.strip__seg.is-on,
	.strip__seg.is-on:hover {
		background: var(--accent);
	}

	.strip__seg:active {
		background: var(--fg-muted);
	}

	.strip__caption {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-4);
		min-height: 1lh;
		padding-top: var(--space-3);
		font-variant-numeric: tabular-nums;
		font-size: var(--text-sm);
	}

	.strip__which {
		font-weight: 500;
	}

	.strip__cmd {
		flex: 1 1 20rem;
		min-width: 0;
		overflow: hidden;
		padding: 0;
		background: none;
		color: var(--fg-muted);
		font-size: var(--text-xs);
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.cmd-cell {
		display: block;
		max-width: 28rem;
		overflow: hidden;
		color: var(--fg-muted);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.history {
		margin: 0;
		padding: 0;
		list-style: none;
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
	}

	.history li {
		display: grid;
		grid-template-columns: 2rem minmax(0, 1fr) auto;
		gap: var(--space-3);
		align-items: baseline;
		padding: 0.625rem var(--space-4);
		border-bottom: 1px solid var(--border);
	}

	.history li:last-child {
		border-bottom: 0;
	}

	.history__step {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	.history__body {
		display: grid;
		gap: 0.125rem;
		min-width: 0;
		font-size: var(--text-xs);
	}

	.history__cmd {
		padding: 0;
		background: none;
		color: var(--fg);
		font-size: var(--text-xs);
		line-height: 1.6;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}

	.history--empty .history__cmd {
		color: var(--fg-muted);
	}

	.history__size {
		color: var(--fg-muted);
		font-size: var(--text-sm);
		font-variant-numeric: tabular-nums;
	}

	.raw {
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
		overflow: hidden;
	}

	.raw__head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-2) var(--space-2) var(--space-2) var(--space-4);
		border-bottom: 1px solid var(--border);
		background: var(--bg-sidebar);
		font-size: var(--text-xs);
		min-width: 0;
	}

	.raw__head .subtle {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	pre {
		margin: 0;
		padding: var(--space-4);
		max-height: 36rem;
		overflow: auto;
		color: var(--fg-muted);
		font-size: var(--text-xs);
		line-height: 1.6;
	}

	@media (max-width: 40rem) {
		.col-cmd,
		.col-digest {
			display: none;
		}
	}
</style>
