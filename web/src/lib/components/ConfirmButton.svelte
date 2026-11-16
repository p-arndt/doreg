<script lang="ts">
	import Button from './Button.svelte';
	import type { IconName } from './Icon.svelte';

	// Two-step destructive action inline: no modal, the second click is the confirmation.
	let {
		label,
		confirmLabel = 'Delete',
		question = 'Are you sure?',
		onconfirm,
		size = 'sm',
		icon,
		iconOnly = false
	}: {
		label: string;
		confirmLabel?: string;
		question?: string;
		onconfirm: () => Promise<void> | void;
		size?: 'sm' | 'md';
		icon?: IconName;
		iconOnly?: boolean;
	} = $props();
	let armed = $state(false);
	let busy = $state(false);

	async function run() {
		busy = true;
		try {
			await onconfirm();
		} finally {
			busy = false;
			armed = false;
		}
	}
</script>

{#if armed}
	<span class="confirm" role="group" aria-label={question}>
		<span class="confirm__q">{question}</span>
		<Button variant="danger-solid" {size} loading={busy} onclick={run}>{confirmLabel}</Button>
		<Button variant="ghost" {size} icon="x" label="Cancel" onclick={() => (armed = false)} />
	</span>
{:else if iconOnly}
	<Button variant="ghost" {size} {icon} {label} onclick={() => (armed = true)} />
{:else}
	<Button variant="danger" {size} {icon} onclick={() => (armed = true)}>{label}</Button>
{/if}

<style>
	.confirm {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		white-space: nowrap;
	}

	.confirm__q {
		margin-right: var(--space-1);
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	/* The group's aria-label still carries the question on narrow screens. */
	@media (max-width: 40rem) {
		.confirm__q {
			display: none;
		}
	}
</style>
