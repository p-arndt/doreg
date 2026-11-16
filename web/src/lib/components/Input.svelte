<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';
	import Icon, { type IconName } from './Icon.svelte';

	let {
		value = $bindable(),
		icon,
		mono = false,
		size = 'md',
		class: cls = '',
		...rest
	}: Omit<HTMLInputAttributes, 'size'> & {
		icon?: IconName;
		mono?: boolean;
		size?: 'md' | 'sm';
	} = $props();
</script>

<span class="input {cls}" class:input--sm={size === 'sm'} class:input--icon={!!icon}>
	{#if icon}<Icon name={icon} size={14} class="input__icon" />{/if}
	<input class:mono bind:value {...rest} />
</span>

<style>
	.input {
		position: relative;
		display: flex;
		align-items: center;
		min-width: 0;
	}

	input {
		width: 100%;
		height: var(--control-h);
		padding: 0 var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius);
		background: var(--bg);
		color: var(--fg);
		font-size: var(--text-base);
		transition: border-color var(--dur-micro) var(--ease);
	}

	input.mono {
		font-size: var(--text-sm);
	}

	.input--sm input {
		height: var(--control-h-sm);
		font-size: var(--text-sm);
	}

	.input--icon input {
		padding-left: 2rem;
	}

	.input :global(.input__icon) {
		position: absolute;
		left: 0.625rem;
		color: var(--fg-subtle);
		pointer-events: none;
	}

	input::placeholder {
		color: var(--fg-subtle);
	}

	input:hover:not(:disabled) {
		border-color: var(--border-hover);
	}

	input:focus {
		outline: 2px solid var(--border-focus);
		outline-offset: -1px;
		border-color: var(--border-strong);
	}

	input:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	input[aria-invalid='true'] {
		border-color: var(--danger);
	}

	input[type='search']::-webkit-search-cancel-button {
		filter: grayscale(1) invert(0.6);
	}
</style>
