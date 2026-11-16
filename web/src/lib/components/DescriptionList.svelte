<script lang="ts">
	// Key/value rows, e.g. image configuration. `mono` renders values (and optionally keys) as code.
	let {
		items,
		mono = false,
		monoKeys = false
	}: { items: [string, string][]; mono?: boolean; monoKeys?: boolean } = $props();
</script>

<dl class="dl">
	{#each items as [k, v], i (i)}
		<div class="dl__row">
			<dt class:mono={monoKeys}>{k}</dt>
			<dd class:mono>{v}</dd>
		</div>
	{/each}
</dl>

<style>
	.dl {
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
	}

	.dl__row {
		display: grid;
		grid-template-columns: minmax(7rem, 30%) 1fr;
		gap: var(--space-4);
		padding: 0.625rem var(--space-4);
		border-bottom: 1px solid var(--border);
	}

	.dl__row:last-child {
		border-bottom: 0;
	}

	dt {
		color: var(--fg-subtle);
		overflow-wrap: anywhere;
	}

	dd {
		min-width: 0;
		overflow-wrap: anywhere;
	}

	.mono {
		font-size: var(--text-sm);
	}

	@media (max-width: 40rem) {
		.dl__row {
			grid-template-columns: 1fr;
			gap: 0.125rem;
		}
	}
</style>
