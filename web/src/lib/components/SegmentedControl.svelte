<script lang="ts" generics="T extends string">
	// A small two-or-three way switch, e.g. what copy buttons copy.
	let {
		value,
		options,
		label,
		onchange
	}: {
		value: T;
		options: { value: T; label: string; title?: string }[];
		label: string;
		onchange: (v: T) => void;
	} = $props();
</script>

<div class="seg" role="radiogroup" aria-label={label}>
	{#each options as o (o.value)}
		<button
			type="button"
			role="radio"
			aria-checked={o.value === value}
			title={o.title}
			onclick={() => onchange(o.value)}>{o.label}</button
		>
	{/each}
</div>

<style>
	.seg {
		display: inline-flex;
		gap: 2px;
		padding: 2px;
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg-sidebar);
	}

	button {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		height: 1.625rem;
		padding: 0 var(--space-3);
		border: 0;
		border-radius: var(--radius-xs);
		background: none;
		color: var(--fg-muted);
		font-size: var(--text-sm);
		font-weight: 500;
		white-space: nowrap;
		cursor: pointer;
		transition:
			color var(--dur-micro) var(--ease),
			background-color var(--dur-micro) var(--ease);
	}

	button:hover {
		color: var(--fg);
	}

	/* The chosen option lifts onto a raised chip. */
	button[aria-checked='true'],
	button[aria-checked='true']:hover {
		background: var(--surface-raised);
		box-shadow:
			0 0 0 1px var(--border-strong),
			var(--shadow-control);
		color: var(--fg);
	}

	button:focus-visible {
		outline: 2px solid var(--border-focus);
		outline-offset: 1px;
	}
</style>
