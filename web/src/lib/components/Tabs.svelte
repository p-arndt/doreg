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
		gap: var(--space-5);
		margin-bottom: var(--space-5);
		border-bottom: 1px solid var(--border);
		overflow-x: auto;
		scrollbar-width: none;
	}

	a {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) 0 0.625rem;
		margin-bottom: -1px;
		border-bottom: 2px solid transparent;
		color: var(--fg-muted);
		font-weight: 500;
		white-space: nowrap;
	}

	a:hover {
		color: var(--fg);
		text-decoration: none;
	}

	a[aria-current='page'] {
		color: var(--fg);
		border-bottom-color: var(--accent);
	}

	a[aria-current='page'] .tabs__count {
		background: var(--accent-soft);
		color: var(--accent-fg);
	}

	.tabs__count {
		min-width: 1.25rem;
		padding: 0 0.375rem;
		border-radius: var(--radius-sm);
		background: var(--surface-active);
		color: var(--fg-muted);
		font-size: var(--text-xs);
		font-variant-numeric: tabular-nums;
		line-height: 1.125rem;
		text-align: center;
	}
</style>
