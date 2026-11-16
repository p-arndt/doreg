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
		padding: 2px;
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg-sidebar);
	}

	button {
		height: 1.5rem;
		padding: 0 var(--space-2);
		border: 0;
		border-radius: var(--radius-sm);
		background: none;
		color: var(--fg-subtle);
		font-size: var(--text-xs);
		font-weight: 500;
		white-space: nowrap;
		cursor: pointer;
	}

	button:hover {
		color: var(--fg);
	}

	button[aria-checked='true'] {
		background: var(--accent-soft);
		color: var(--accent-fg);
		box-shadow: inset 0 0 0 1px var(--accent-line);
	}
</style>
