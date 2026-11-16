<script lang="ts">
	import type { Snippet } from 'svelte';

	// Rim's measurement strip: labelled values in a row, parted by vertical hairlines.
	// Use <Fact> children; a Fact marked `measure` sets its value in mono.
	let { children, label }: { children: Snippet; label?: string } = $props();
</script>

<dl class="facts" aria-label={label}>
	{@render children()}
</dl>

<style>
	.facts {
		display: flex;
		flex-wrap: wrap;
		margin: 0;
		border-top: 1px solid var(--border-strong);
		border-bottom: 1px solid var(--border-strong);
	}

	.facts :global(.fact) {
		display: grid;
		gap: 0.125rem;
		min-width: 0;
		padding: var(--space-3) var(--space-5) var(--space-3) 0;
		margin-right: var(--space-5);
		border-right: 1px solid var(--border);
	}

	.facts :global(.fact:last-child) {
		border-right: 0;
		margin-right: 0;
	}
</style>
