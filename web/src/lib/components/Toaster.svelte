<script lang="ts">
	import { fly } from 'svelte/transition';
	import { toasts, dismiss } from '#lib/toast.svelte.js';
	import Icon from './Icon.svelte';
</script>

<div class="toaster" role="status" aria-live="polite">
	{#each toasts as t (t.id)}
		<div class="toast toast--{t.tone}" transition:fly={{ y: 8, duration: 160 }}>
			<Icon name={t.tone === 'danger' ? 'alert' : 'check'} size={16} />
			<p>{t.message}</p>
			<button type="button" aria-label="Dismiss" onclick={() => dismiss(t.id)}>
				<Icon name="x" size={14} />
			</button>
		</div>
	{/each}
</div>

<style>
	.toaster {
		position: fixed;
		right: var(--space-5);
		bottom: var(--space-5);
		z-index: 50;
		display: grid;
		gap: var(--space-2);
		width: 24rem;
		max-width: calc(100% - 2 * var(--space-4));
		pointer-events: none;
	}

	.toast {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-3);
		border-radius: var(--radius-lg);
		background: var(--surface-raised);
		box-shadow: var(--shadow-pop);
		pointer-events: auto;
	}

	.toast > :global(.icon) {
		margin-top: 0.125rem;
	}

	/* The accent bar marks the tone at a glance without tinting the whole toast. */
	.toast--success {
		box-shadow:
			var(--shadow-pop),
			inset 2px 0 0 var(--accent);
	}

	.toast--danger {
		box-shadow:
			var(--shadow-pop),
			inset 2px 0 0 var(--danger);
	}

	.toast--success > :global(.icon) {
		color: var(--accent);
	}

	.toast--danger > :global(.icon) {
		color: var(--danger);
	}

	p {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}

	button {
		display: grid;
		place-items: center;
		width: 1.25rem;
		height: 1.25rem;
		padding: 0;
		border: 0;
		border-radius: var(--radius-sm);
		background: none;
		color: var(--fg-subtle);
		cursor: pointer;
	}

	button:hover {
		color: var(--fg);
		background: var(--surface-hover);
	}

	@media (max-width: 40rem) {
		.toaster {
			right: var(--space-4);
			bottom: var(--space-4);
		}
	}
</style>
