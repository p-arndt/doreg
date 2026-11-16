<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLSelectAttributes } from 'svelte/elements';
	import Icon from './Icon.svelte';

	let {
		value = $bindable(),
		size = 'md',
		children,
		class: cls = '',
		...rest
	}: Omit<HTMLSelectAttributes, 'size'> & { size?: 'md' | 'sm'; children: Snippet } = $props();
</script>

<span class="select {cls}" class:select--sm={size === 'sm'}>
	<select bind:value {...rest}>{@render children()}</select>
	<Icon name="chevronDown" size={14} class="select__chevron" />
</span>

<style>
	.select {
		position: relative;
		display: inline-flex;
		align-items: center;
		min-width: 0;
	}

	select {
		appearance: none;
		width: 100%;
		height: var(--control-h);
		padding: 0 2rem 0 var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius);
		background: var(--bg);
		color: var(--fg);
		font-size: var(--text-base);
		cursor: pointer;
	}

	.select--sm select {
		height: var(--control-h-sm);
		padding-left: var(--space-2);
		font-size: var(--text-sm);
	}

	select:hover:not(:disabled) {
		border-color: var(--border-hover);
	}

	select:focus {
		outline: 2px solid var(--border-focus);
		outline-offset: -1px;
		border-color: var(--border-strong);
	}

	select:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.select :global(.select__chevron) {
		position: absolute;
		right: 0.625rem;
		color: var(--fg-subtle);
		pointer-events: none;
	}
</style>
