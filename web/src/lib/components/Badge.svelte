<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		tone = 'neutral',
		title,
		children
	}: {
		tone?: 'neutral' | 'accent' | 'success' | 'warning' | 'danger';
		title?: string;
		children: Snippet;
	} = $props();
</script>

<span class="badge badge--{tone}" {title}>{@render children()}</span>

<style>
	.badge {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		height: 1.25rem;
		padding: 0 0.4375rem;
		border-radius: var(--radius-sm);
		font-size: var(--text-xs);
		font-weight: 500;
		line-height: 1;
		white-space: nowrap;
		vertical-align: middle;
	}

	.badge--neutral {
		background: var(--surface-active);
		color: var(--fg-muted);
	}

	.badge--accent {
		background: var(--accent-soft);
		color: var(--accent-fg);
		box-shadow: inset 0 0 0 1px var(--accent-line);
	}

	.badge--success {
		background: var(--success-soft);
		color: var(--success);
	}

	.badge--warning {
		background: var(--warning-soft);
		color: var(--warning);
	}

	.badge--danger {
		background: var(--danger-soft);
		color: var(--danger);
	}
</style>
