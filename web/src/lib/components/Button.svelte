<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import Icon, { type IconName } from './Icon.svelte';

	type Props = HTMLButtonAttributes & {
		variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-solid';
		size?: 'md' | 'sm';
		/** Renders a link styled as a button. */
		href?: string;
		/** Full page load instead of client-side routing, for server endpoints like SSO. */
		reload?: boolean;
		icon?: IconName;
		/** Icon-only button; `label` becomes the accessible name. */
		label?: string;
		loading?: boolean;
		children?: Snippet;
	};

	let {
		variant = 'secondary',
		size = 'md',
		href,
		reload = false,
		icon,
		label,
		loading = false,
		type = 'button',
		disabled,
		children,
		class: cls = '',
		...rest
	}: Props = $props();

	const iconOnly = $derived(!children && !!label);
</script>

{#if href}
	<a
		{href}
		data-sveltekit-reload={reload || undefined}
		class="btn btn--{variant} btn--{size} {cls}"
		class:btn--icon={iconOnly}
		aria-label={iconOnly ? label : undefined}
		title={iconOnly ? label : undefined}
	>
		{#if icon}<Icon name={icon} size={size === 'sm' ? 14 : 16} />{/if}
		{@render children?.()}
	</a>
{:else}
	<button
		{type}
		class="btn btn--{variant} btn--{size} {cls}"
		class:btn--icon={iconOnly}
		disabled={disabled || loading}
		aria-busy={loading || undefined}
		aria-label={iconOnly ? label : undefined}
		title={iconOnly ? label : undefined}
		{...rest}
	>
		{#if loading}
			<span class="spinner" aria-hidden="true"></span>
		{:else if icon}
			<Icon name={icon} size={size === 'sm' ? 14 : 16} />
		{/if}
		{@render children?.()}
	</button>
{/if}

<style>
	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		height: var(--control-h);
		padding: 0 var(--space-3);
		border: 1px solid transparent;
		border-radius: var(--radius);
		font-size: var(--text-base);
		font-weight: 500;
		line-height: 1;
		white-space: nowrap;
		cursor: pointer;
		text-decoration: none;
		transition:
			background-color var(--dur-micro) var(--ease),
			border-color var(--dur-micro) var(--ease),
			color var(--dur-micro) var(--ease),
			transform var(--dur-micro) var(--ease);
	}

	.btn:hover {
		text-decoration: none;
	}

	.btn--sm {
		height: var(--control-h-sm);
		padding: 0 var(--space-2);
		font-size: var(--text-sm);
		gap: 0.375rem;
	}

	.btn--icon {
		width: var(--control-h);
		padding: 0;
	}

	.btn--icon.btn--sm {
		width: var(--control-h-sm);
	}

	.btn--primary {
		background: var(--accent);
		color: var(--fg-on-accent);
		font-weight: 600;
		box-shadow: var(--shadow-control);
	}

	.btn--primary:hover:not(:disabled) {
		background: var(--accent-hover);
	}

	.btn--secondary {
		background: var(--surface-raised);
		border-color: var(--border-strong);
		color: var(--fg);
		box-shadow: var(--shadow-control);
	}

	.btn--secondary:hover:not(:disabled) {
		border-color: var(--border-hover);
		background: color-mix(in oklab, var(--surface-raised), var(--color-ink) 4%);
	}

	.btn--ghost {
		background: transparent;
		color: var(--fg-muted);
	}

	.btn--ghost:hover:not(:disabled) {
		background: var(--surface-hover);
		color: var(--fg);
	}

	.btn--danger {
		background: transparent;
		color: var(--danger);
	}

	.btn--danger:hover:not(:disabled) {
		background: var(--danger-soft);
	}

	.btn--danger-solid {
		background: var(--danger-solid);
		color: var(--danger-on);
	}

	.btn--danger-solid:hover:not(:disabled) {
		background: var(--danger-solid-hover);
	}

	.btn:active:not(:disabled) {
		transform: scale(0.98);
	}

	.btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.btn[aria-busy='true'] {
		cursor: progress;
	}

	.spinner {
		width: 0.875rem;
		height: 0.875rem;
		border: 1.5px solid currentColor;
		border-right-color: transparent;
		border-radius: 50%;
		animation: spin 0.7s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
</style>
