<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	// Inline message inside a form or section; ErrorState is for a whole data region failing.
	let {
		tone = 'neutral',
		children
	}: { tone?: 'neutral' | 'danger' | 'warning'; children: Snippet } = $props();
</script>

<p class="notice notice--{tone}" role={tone === 'danger' ? 'alert' : undefined}>
	{#if tone !== 'neutral'}<Icon name="alert" size={14} />{/if}
	<span>{@render children()}</span>
</p>

<style>
	.notice {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-lg);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}

	.notice :global(.icon) {
		margin-top: 0.1875rem;
	}

	.notice--neutral {
		background: var(--surface-raised);
		color: var(--fg-muted);
	}

	.notice--danger {
		background: var(--danger-soft);
		color: var(--danger);
	}

	.notice--warning {
		background: var(--warning-soft);
		color: var(--warning);
	}
</style>
