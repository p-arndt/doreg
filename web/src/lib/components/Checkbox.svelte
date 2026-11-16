<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLInputAttributes } from 'svelte/elements';

	let {
		checked = $bindable(false),
		label,
		description,
		...rest
	}: Omit<HTMLInputAttributes, 'type'> & {
		label: string;
		description?: string | Snippet;
	} = $props();
</script>

<label class="check">
	<input type="checkbox" bind:checked {...rest} />
	<span class="check__text">
		<span class="check__label">{label}</span>
		{#if typeof description === 'string'}
			<span class="check__desc">{description}</span>
		{:else if description}
			<span class="check__desc">{@render description()}</span>
		{/if}
	</span>
</label>

<style>
	.check {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		cursor: pointer;
		min-width: 0;
	}

	.check__text {
		display: grid;
		gap: 0.125rem;
	}

	.check__label {
		font-size: var(--text-base);
		line-height: 1.25rem;
	}

	.check__desc {
		font-size: var(--text-xs);
		color: var(--fg-subtle);
	}

	input {
		appearance: none;
		flex: none;
		width: 1rem;
		height: 1rem;
		margin: 0.125rem 0 0;
		border: 1px solid var(--border-hover);
		border-radius: var(--radius-sm);
		background: var(--surface);
		display: grid;
		place-content: center;
		cursor: pointer;
		transition:
			background-color 120ms var(--ease),
			border-color 120ms var(--ease);
	}

	input::after {
		content: '';
		width: 0.55rem;
		height: 0.3rem;
		border: 1.75px solid var(--fg-on-accent);
		border-top: 0;
		border-right: 0;
		transform: translateY(-1px) rotate(-45deg) scale(0);
		transition: transform 120ms var(--ease);
	}

	input:hover:not(:checked) {
		border-color: var(--accent-line);
	}

	input:checked {
		background: var(--accent);
		border-color: var(--accent);
	}

	input:checked::after {
		transform: translateY(-1px) rotate(-45deg) scale(1);
	}

	input:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
