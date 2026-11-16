<script lang="ts" generics="T extends string">
	import { Select } from 'bits-ui';
	import Icon from './Icon.svelte';

	// A styled single select on bits-ui (the primitive shadcn-svelte builds on). Never a native
	// <select>: its menu is drawn by the OS and ignores the design.
	let {
		value = $bindable(),
		options,
		onchange,
		size = 'md',
		placeholder = 'Select…',
		disabled = false,
		name,
		class: cls = '',
		'aria-label': ariaLabel
	}: {
		value?: T;
		options: { value: T; label: string; disabled?: boolean }[];
		onchange?: (value: T) => void;
		size?: 'md' | 'sm';
		placeholder?: string;
		disabled?: boolean;
		name?: string;
		class?: string;
		'aria-label'?: string;
	} = $props();

	// bits-ui treats an empty value as "nothing selected", so an "All …" option with value '' gets a stand-in key.
	const EMPTY = '__empty__';
	const key = (v: string | undefined) => (v === '' ? EMPTY : (v ?? ''));
	const items = $derived(options.map((o) => ({ ...o, value: key(o.value) })));
	const selected = $derived(options.find((o) => o.value === value));
</script>

<Select.Root
	type="single"
	{disabled}
	{name}
	{items}
	value={key(value)}
	onValueChange={(v) => {
		const next = (v === EMPTY ? '' : v) as T;
		value = next;
		onchange?.(next);
	}}
>
	<Select.Trigger class="select select--{size} {cls}" aria-label={ariaLabel}>
		<span class="select__value" class:select__value--empty={!selected}
			>{selected?.label ?? placeholder}</span
		>
		<Icon name="chevronDown" size={14} class="select__chevron" />
	</Select.Trigger>
	<Select.Portal>
		<Select.Content class="select-menu" sideOffset={6} align="start">
			<Select.Viewport class="select-menu__viewport">
				{#each options as o (o.value)}
					<Select.Item
						class="select-menu__item"
						value={key(o.value)}
						label={o.label}
						disabled={o.disabled}
					>
						{#snippet children({ selected: isSelected })}
							<span class="select-menu__label">{o.label}</span>
							{#if isSelected}<Icon name="check" size={14} class="select-menu__check" />{/if}
						{/snippet}
					</Select.Item>
				{/each}
			</Select.Viewport>
		</Select.Content>
	</Select.Portal>
</Select.Root>

<style>
	/* bits-ui renders the trigger and the portalled menu itself, so the styles reach them globally. */
	:global(.select) {
		display: inline-flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		min-width: 0;
		height: var(--control-h);
		padding: 0 var(--space-2) 0 var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius);
		background: var(--bg);
		color: var(--fg);
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
		transition: border-color var(--dur-micro) var(--ease);
	}

	:global(.select--sm) {
		height: var(--control-h-sm);
		padding-left: var(--space-2);
		font-size: var(--text-sm);
	}

	:global(.select:hover:not(:disabled)) {
		border-color: var(--border-hover);
	}

	:global(.select:focus-visible) {
		outline: 2px solid var(--border-focus);
		outline-offset: -1px;
	}

	:global(.select[data-state='open']) {
		border-color: var(--border-focus);
	}

	:global(.select:disabled) {
		opacity: 0.55;
		cursor: not-allowed;
	}

	:global(.select__value) {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	:global(.select__value--empty) {
		color: var(--fg-subtle);
	}

	:global(.select__chevron) {
		color: var(--fg-subtle);
	}

	:global(.select-menu) {
		z-index: 60;
		min-width: var(--bits-select-anchor-width);
		max-height: var(--bits-select-content-available-height);
		padding: var(--space-1);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-lg);
		background: var(--surface-raised);
		box-shadow: var(--shadow-pop);
		outline: none;
	}

	:global(.select-menu[data-state='open']) {
		animation: select-in var(--dur-micro) var(--ease);
	}

	:global(.select-menu__item) {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		height: 2rem;
		padding: 0 var(--space-2);
		border-radius: var(--radius-sm);
		color: var(--fg-muted);
		font-size: var(--text-base);
		cursor: pointer;
		outline: none;
		user-select: none;
	}

	:global(.select-menu__item[data-highlighted]) {
		background: var(--surface-active);
		color: var(--fg);
	}

	:global(.select-menu__item[data-selected]) {
		color: var(--fg);
	}

	:global(.select-menu__item[data-disabled]) {
		opacity: 0.55;
		cursor: not-allowed;
	}

	:global(.select-menu__check) {
		color: var(--accent);
	}

	@keyframes select-in {
		from {
			opacity: 0;
			transform: translateY(-2px);
		}
		to {
			opacity: 1;
			transform: none;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		:global(.select-menu[data-state='open']) {
			animation: none;
		}
	}
</style>
