<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon, { type IconName } from './Icon.svelte';

	let {
		title,
		description,
		icon,
		children
	}: { title: string; description?: string; icon?: IconName; children?: Snippet } = $props();
</script>

<div class="empty ticks">
	{#if icon}<span class="empty__icon"><Icon name={icon} size={18} /></span>{/if}
	<h2 class="empty__title">{title}</h2>
	{#if description}<p class="empty__desc">{description}</p>{/if}
	{#if children}<div class="empty__actions">{@render children()}</div>{/if}
</div>

<style>
	.empty {
		display: grid;
		justify-items: center;
		gap: var(--space-2);
		padding: var(--space-7) var(--space-5);
		background: var(--surface);
		text-align: center;
	}

	.empty__icon {
		display: grid;
		place-items: center;
		width: 2.25rem;
		height: 2.25rem;
		margin-bottom: var(--space-1);
		border-radius: var(--radius);
		background: var(--accent-soft);
		color: var(--accent-fg);
	}

	.empty__title {
		font-size: var(--text-base);
	}

	.empty__desc {
		max-width: 26rem;
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.empty__actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
		max-width: 100%;
	}
</style>
