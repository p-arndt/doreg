<script lang="ts">
	import Button from './Button.svelte';
	import Icon from './Icon.svelte';

	let { error, retry }: { error: unknown; retry?: () => void } = $props();
	const message = $derived(error instanceof Error ? error.message : String(error));
</script>

<div class="error" role="alert">
	<Icon name="alert" size={16} />
	<div class="error__body">
		<p class="error__title">Could not load this</p>
		<p class="error__msg">{message}</p>
	</div>
	{#if retry}<Button size="sm" onclick={retry}>Try again</Button>{/if}
</div>

<style>
	.error {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-4);
		border: 1px solid var(--danger-line);
		border-radius: var(--radius-lg);
		background: var(--danger-soft);
		color: var(--danger);
	}

	.error__body {
		flex: 1;
		min-width: 0;
	}

	.error__title {
		font-weight: 500;
		color: var(--fg);
	}

	.error__msg {
		color: var(--fg-muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
</style>
