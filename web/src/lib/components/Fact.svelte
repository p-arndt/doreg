<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		label,
		measure = false,
		children
	}: { label: string; measure?: boolean; children: Snippet } = $props();
</script>

<div class="fact">
	<dt>{label}</dt>
	<dd class:measure>{@render children()}</dd>
</div>

<style>
	dt {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
		font-weight: 500;
	}

	dd {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-1);
		margin: 0;
		color: var(--fg);
		font-size: var(--text-base);
	}

	dd.measure {
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		font-variant-numeric: tabular-nums;
	}
</style>
