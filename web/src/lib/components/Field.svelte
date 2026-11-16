<script lang="ts">
	import type { Snippet } from 'svelte';

	// Wraps one control with its label and help text. The label element wraps the control, so no ids are needed.
	let {
		label,
		hint,
		error,
		children
	}: { label: string; hint?: string | Snippet; error?: string; children: Snippet } = $props();
</script>

<label class="field">
	<span class="field__label">{label}</span>
	{@render children()}
	{#if error}
		<span class="field__error" role="alert">{error}</span>
	{:else if typeof hint === 'string'}
		<span class="field__hint">{hint}</span>
	{:else if hint}
		<span class="field__hint">{@render hint()}</span>
	{/if}
</label>

<style>
	.field {
		display: grid;
		gap: 0.375rem;
		min-width: 0;
	}

	.field__label {
		font-size: var(--text-sm);
		font-weight: 500;
		color: var(--fg);
	}

	.field__hint {
		font-size: var(--text-xs);
		color: var(--fg-subtle);
	}

	.field__error {
		font-size: var(--text-xs);
		color: var(--danger);
	}
</style>
