<script lang="ts">
	import { platform } from '#lib/format.js';
	import type { ManifestSummary } from '#lib/types.js';

	let { m }: { m: ManifestSummary } = $props();
</script>

<span class="platforms">
	{#each m.platforms as p, i (p.digest + i)}
		<span>{platform(p)}</span>
	{:else}
		{#if m.artifactType}
			<span title={m.artifactType}>artifact</span>
		{:else}
			<span class="subtle">unknown</span>
		{/if}
	{/each}
	{#if m.attestations > 0}
		<span class="subtle" title="Build attestations (SBOM, provenance)"
			>+{m.attestations} attestation{m.attestations > 1 ? 's' : ''}</span
		>
	{/if}
</span>

<style>
	.platforms {
		display: inline-flex;
		flex-wrap: wrap;
		gap: 0.125rem var(--space-2);
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.platforms > span:not(:last-child)::after {
		content: ',';
		color: var(--fg-subtle);
	}
</style>
