<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title,
		description,
		actions,
		children
	}: { title: string; description?: string; actions?: Snippet; children: Snippet } = $props();
</script>

<section class="section">
	<header class="section__head">
		<div class="section__text">
			<h2>{title}</h2>
			{#if description}<p class="section__desc">{description}</p>{/if}
		</div>
		{#if actions}<div class="section__actions">{@render actions()}</div>{/if}
	</header>
	{@render children()}
</section>

<style>
	.section + :global(.section) {
		margin-top: var(--space-7);
	}

	.section__head {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: var(--space-3);
		flex-wrap: wrap;
		margin-bottom: var(--space-3);
	}

	.section__text {
		display: grid;
		gap: 0.125rem;
		min-width: 0;
	}

	.section__desc {
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.section__actions {
		display: flex;
		gap: var(--space-2);
		align-items: center;
	}
</style>
