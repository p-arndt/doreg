<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	let {
		title,
		crumbs = [],
		description,
		actions,
		meta
	}: {
		title: string;
		crumbs?: { href: string; label: string }[];
		description?: string | Snippet;
		/** Buttons on the right. */
		actions?: Snippet;
		/** A line under the title for badges or facts. */
		meta?: Snippet;
	} = $props();
</script>

<header class="ph">
	{#if crumbs.length}
		<nav class="ph__crumbs" aria-label="Breadcrumb">
			{#each crumbs as c (c.href)}
				<a href={c.href}>{c.label}</a>
				<Icon name="chevronRight" size={12} />
			{/each}
		</nav>
	{/if}
	<div class="ph__row">
		<div class="ph__text">
			<h1>{title}</h1>
			{#if typeof description === 'string'}
				<p class="ph__desc">{description}</p>
			{:else if description}
				<p class="ph__desc">{@render description()}</p>
			{/if}
			{#if meta}<div class="ph__meta">{@render meta()}</div>{/if}
		</div>
		{#if actions}<div class="ph__actions">{@render actions()}</div>{/if}
	</div>
</header>

<style>
	.ph {
		margin-bottom: var(--space-6);
	}

	.ph__crumbs {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-1);
		margin-bottom: var(--space-2);
		color: var(--fg-subtle);
		font-size: var(--text-sm);
	}

	.ph__crumbs a {
		color: var(--fg-muted);
	}

	.ph__crumbs a:hover {
		color: var(--fg);
		text-decoration: none;
	}

	.ph__row {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-4);
		flex-wrap: wrap;
	}

	.ph__text {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: var(--space-1);
		min-width: 0;
		flex: 1 1 20rem;
	}

	h1 {
		overflow-wrap: anywhere;
		text-wrap: balance;
	}

	.ph__desc {
		color: var(--fg-muted);
	}

	.ph__meta {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-3);
		margin-top: var(--space-1);
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.ph__actions {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
</style>
