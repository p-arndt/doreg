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

<div class="empty">
	<div class="empty__head">
		{#if icon}<Icon name={icon} size={16} />{/if}
		<h2 class="empty__title">{title}</h2>
	</div>
	{#if description}<p class="empty__desc">{description}</p>{/if}
	{#if children}<div class="empty__actions">{@render children()}</div>{/if}
</div>

<style>
	.empty {
		display: grid;
		justify-items: center;
		gap: var(--space-2);
		padding: var(--space-7) var(--space-5);
		border: 1px dashed var(--border-strong);
		border-radius: var(--radius-lg);
		text-align: center;
	}

	.empty__head {
		display: grid;
		justify-items: center;
		gap: var(--space-3);
	}

	.empty__head :global(.icon) {
		box-sizing: content-box;
		padding: var(--space-2);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius);
		background: var(--surface-raised);
		color: var(--fg-muted);
	}

	.empty__title {
		font-size: var(--text-base);
	}

	.empty__desc {
		max-width: 34rem;
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
