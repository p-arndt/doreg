<script lang="ts">
	import type { Snippet } from 'svelte';

	// A quiet metadata panel: labelled values on one grid, labels above values, every value the same size.
	// Use <Fact> children.
	let { children, label }: { children: Snippet; label?: string } = $props();
</script>

<dl class="facts" aria-label={label}>
	{@render children()}
</dl>

<style>
	.facts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-4) var(--space-7);
		margin: 0;
		padding: var(--space-4) var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
		box-shadow: var(--shadow-control);
	}

	@media (max-width: 40rem) {
		.facts {
			display: grid;
			grid-template-columns: repeat(2, minmax(0, 1fr));
			gap: var(--space-4);
			padding: var(--space-4);
		}
	}
</style>
