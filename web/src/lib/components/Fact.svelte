<script lang="ts">
	import type { Snippet } from 'svelte';

	// `measure` sets tabular figures; `big` gives the one value a screen is about a medium weight, not a bigger size,
	// so every value in the row shares a baseline.
	let {
		label,
		measure = false,
		big = false,
		children
	}: { label: string; measure?: boolean; big?: boolean; children: Snippet } = $props();
</script>

<div class="fact">
	<dt>{label}</dt>
	<dd class:measure class:big>{@render children()}</dd>
</div>

<style>
	.fact {
		display: grid;
		align-content: start;
		gap: var(--space-1);
		min-width: 0;
	}

	dt {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
		font-weight: 500;
		line-height: 1rem;
	}

	dd {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2);
		min-height: 1.375rem;
		margin: 0;
		color: var(--fg);
		font-size: var(--text-base);
		line-height: 1.375rem;
	}

	dd.measure {
		font-variant-numeric: tabular-nums;
	}

	dd.big {
		font-weight: 600;
	}
</style>
