<script lang="ts">
	import { api, q } from '#lib/api.js';
	import { bytes, count, pullText, refPath, shortDigest } from '#lib/format.js';
	import { prefs } from '#lib/prefs.svelte.js';
	import { configSource, envItems, runtimeItems } from '#lib/imageConfig.js';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import SegmentedControl from '#lib/components/SegmentedControl.svelte';
	import Platforms from '#lib/components/Platforms.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { ImageConfig, ManifestDetail, Repo } from '#lib/types.js';

	// The newest tag at a glance: what it is, how to pull it, and how it runs, without opening it.
	let { repo, tag }: { repo: string; tag: Repo['tags'][number] } = $props();

	const host = location.host;
	const reference = $derived(`${host}/${repo}:${tag.name}`);
	const text = $derived(pullText(reference, prefs.copyMode));
	const source = $derived(configSource(tag.manifest));

	let loaded = $state<{ digest: string; config: ImageConfig | null; failed: boolean } | null>(null);
	$effect(() => {
		const digest = source;
		if (!digest || loaded?.digest === digest) return;
		loaded = { digest, config: null, failed: false };
		api
			.get<ManifestDetail>(`/api/manifest?repo=${q(repo)}&ref=${q(digest)}`)
			.then((d) => {
				if (loaded?.digest === digest) loaded = { digest, config: d.config ?? null, failed: false };
			})
			.catch(() => {
				if (loaded?.digest === digest) loaded = { digest, config: null, failed: true };
			});
	});

	const config = $derived(loaded?.digest === source ? loaded.config : null);
	const pending = $derived(
		!!source && loaded?.digest === source && !loaded.config && !loaded.failed
	);
	// The platform is already in the header line, so the runtime list starts after it.
	const runtime = $derived(config ? runtimeItems(config).filter(([k]) => k !== 'Platform') : []);
	const env = $derived(config ? envItems(config) : []);
	const labels = $derived(Object.entries(config?.config?.Labels ?? {}));

	type SectionKey = 'runtime' | 'env' | 'labels';
	const sections = $derived(
		[
			{ key: 'runtime' as const, label: 'Runtime', rows: runtime },
			{ key: 'env' as const, label: `Env ${env.length}`, rows: env },
			{ key: 'labels' as const, label: `Labels ${labels.length}`, rows: labels }
		].filter((s) => s.rows.length)
	);
	let picked = $state<SectionKey>('runtime');
	const shown = $derived(sections.find((s) => s.key === picked)?.key ?? sections[0]?.key);
	const rows = $derived(sections.find((s) => s.key === shown)?.rows ?? []);
</script>

<section class="latest" aria-label="Latest push">
	<div class="latest__main">
		<header class="latest__head">
			<div class="latest__title">
				<Badge tone="accent">Latest push</Badge>
				<a class="latest__tag" href={refPath(repo, tag.name)}>{tag.name}</a>
				<span class="digest" title={tag.manifest.digest}>{shortDigest(tag.manifest.digest)}</span>
			</div>
			<Button size="sm" variant="ghost" href={refPath(repo, tag.name)}>Open tag</Button>
		</header>

		<div class="latest__meta">
			<Platforms m={tag.manifest} />
			<span class="dot" aria-hidden="true"></span>
			<span class="measure">{bytes(tag.manifest.size)}</span>
			<span class="dot" aria-hidden="true"></span>
			<span>Pushed <Time unix={tag.updatedAt} /></span>
			<span class="dot" aria-hidden="true"></span>
			<span class="measure" title={tag.manifest.pulls.toLocaleString()}
				>{count(tag.manifest.pulls)} {tag.manifest.pulls === 1 ? 'pull' : 'pulls'}</span
			>
		</div>

		<div class="latest__pull">
			<code title={text}>{text}</code>
			<CopyButton
				value={text}
				text={prefs.copyMode === 'ref' ? 'Copy reference' : 'Copy pull command'}
				label={`Copy ${text}`}
			/>
		</div>
	</div>

	{#if pending}
		<div class="spec"><Skeleton rows={3} columns={[1, 3]} /></div>
	{:else if sections.length}
		<div class="spec">
			<div class="spec__head">
				<span class="spec__title">Configuration</span>
				{#if sections.length > 1}
					<SegmentedControl
						label="Configuration section"
						value={shown}
						options={sections.map((s) => ({ value: s.key, label: s.label }))}
						onchange={(v) => (picked = v)}
					/>
				{/if}
			</div>
			<dl class="spec__rows">
				{#each rows as [k, v], i (i)}
					<div class="spec__row">
						<dt title={k} class:mono={shown !== 'runtime'}>{k}</dt>
						<dd title={v}>{v || '—'}</dd>
					</div>
				{/each}
			</dl>
		</div>
	{/if}
</section>

<style>
	.latest {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: var(--space-4);
		margin-bottom: var(--space-6);
		padding: var(--space-4) var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
		box-shadow: var(--shadow-control);
	}

	.latest__main {
		display: grid;
		align-content: start;
		gap: var(--space-3);
		min-width: 0;
	}

	@media (min-width: 64rem) {
		.latest:has(.spec) {
			grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
			gap: var(--space-6);
		}
	}

	.latest__head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		flex-wrap: wrap;
	}

	.latest__title {
		display: flex;
		align-items: baseline;
		gap: var(--space-3);
		flex-wrap: wrap;
		min-width: 0;
	}

	.latest__title :global(.badge) {
		align-self: center;
	}

	.latest__tag {
		color: var(--fg);
		font-size: var(--text-lg);
		font-weight: 600;
		letter-spacing: -0.01em;
	}

	.latest__meta {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-3);
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.dot {
		width: 3px;
		height: 3px;
		border-radius: 50%;
		background: var(--fg-subtle);
	}

	.latest__pull {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		max-width: 44rem;
		min-height: var(--control-h);
		padding: 0.125rem 0.125rem 0.125rem var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg-sidebar);
	}

	.latest__pull code {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		padding: 0;
		background: none;
		color: var(--fg);
		font-size: var(--text-xs);
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	/* The image's runtime as a spec sheet beside the pull line, so the card reads at a glance. */
	.spec {
		display: grid;
		align-content: start;
		gap: var(--space-2);
		min-width: 0;
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg-sidebar);
	}

	.spec__head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		flex-wrap: wrap;
		min-height: 1.75rem;
	}

	.spec__title {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
		font-weight: 500;
	}

	.spec__rows {
		display: grid;
		max-height: 11rem;
		margin: 0;
		overflow-y: auto;
	}

	.spec__row {
		display: grid;
		grid-template-columns: minmax(5.5rem, 32%) minmax(0, 1fr);
		gap: var(--space-3);
		padding: 0.3125rem 0;
		border-top: 1px solid var(--border);
		font-size: var(--text-xs);
		line-height: 1.5;
	}

	.spec__row:first-child {
		border-top: 0;
	}

	.spec__row dt,
	.spec__row dd {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.spec__row dt {
		color: var(--fg-muted);
	}

	.spec__row dt.mono,
	.spec__row dd {
		font-family: var(--font-mono);
	}

	.spec__row dd {
		margin: 0;
		color: var(--fg);
	}

	@media (max-width: 40rem) {
		.latest {
			padding: var(--space-4);
		}

		.latest__pull code {
			overflow-wrap: anywhere;
			white-space: normal;
		}
	}
</style>
