<script lang="ts">
	// URL-driven tabs: each tab is a link, so the browser history and reloads keep the selection.
	let {
		items,
		active,
		label
	}: {
		items: { key: string; label: string; href: string; count?: number }[];
		active: string;
		label: string;
	} = $props();
</script>

<nav class="tabs" aria-label={label}>
	{#each items as t (t.key)}
		<a href={t.href} aria-current={t.key === active ? 'page' : undefined}>
			{t.label}
			{#if t.count !== undefined}<span class="tabs__count">{t.count}</span>{/if}
		</a>
	{/each}
</nav>

<style>
	.tabs {
		display: flex;
		gap: var(--space-1);
		margin-bottom: var(--space-5);
		border-bottom: 1px solid var(--border);
		overflow-x: auto;
		scrollbar-width: none;
	}

	a {
		position: relative;
		isolation: isolate;
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		height: 2.5rem;
		padding: 0 var(--space-3);
		color: var(--fg-muted);
		font-weight: 500;
		white-space: nowrap;
		transition: color var(--dur-micro) var(--ease);
	}

	a::before {
		content: '';
		position: absolute;
		inset: var(--space-1) 0;
		z-index: -1;
		border-radius: var(--radius);
		background: var(--surface-hover);
		opacity: 0;
		transition: opacity var(--dur-micro) var(--ease);
	}

	a::after {
		content: '';
		position: absolute;
		left: var(--space-3);
		right: var(--space-3);
		bottom: -1px;
		height: 2px;
		border-radius: 2px 2px 0 0;
		background: var(--accent);
		opacity: 0;
		transform: scaleX(0.6);
		transition:
			opacity var(--dur-micro) var(--ease),
			transform var(--dur-short) var(--ease);
	}

	a:hover {
		color: var(--fg);
		text-decoration: none;
	}

	a:hover::before {
		opacity: 1;
	}

	a:focus-visible {
		outline-offset: -2px;
		border-radius: var(--radius);
	}

	a[aria-current='page'] {
		color: var(--fg);
	}

	a[aria-current='page']::after {
		opacity: 1;
		transform: none;
	}

	.tabs__count {
		min-width: 1.25rem;
		padding: 0 0.375rem;
		border-radius: var(--radius-xs);
		background: var(--surface-active);
		color: var(--fg-muted);
		font-size: var(--text-xs);
		font-variant-numeric: tabular-nums;
		line-height: 1.125rem;
		text-align: center;
	}
</style>
