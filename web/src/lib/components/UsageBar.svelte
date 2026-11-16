<script lang="ts">
	import { bytes, fraction } from '#lib/format.js';

	// Storage used against an optional quota: a thin bar plus "1.2 GB of 5 GB".
	let { used, quota }: { used: number; quota: number } = $props();
	const share = $derived(fraction(used, quota));
	const tone = $derived(share >= 0.95 ? 'danger' : share >= 0.8 ? 'warning' : 'accent');
</script>

<span class="usage">
	<span class="usage__text measure">
		{bytes(used)}{#if quota}<span class="subtle"> of {bytes(quota)}</span>{/if}
	</span>
	{#if quota}
		<span
			class="usage__bar"
			role="meter"
			aria-label="Quota used"
			aria-valuemin={0}
			aria-valuemax={quota}
			aria-valuenow={used}
		>
			<span class="usage__fill usage__fill--{tone}" style:width="{share * 100}%"></span>
		</span>
	{/if}
</span>

<style>
	.usage {
		display: inline-grid;
		gap: 0.3125rem;
		min-width: 7rem;
	}

	.usage__bar {
		display: block;
		height: 4px;
		border-radius: 0;
		background: var(--track);
		overflow: hidden;
	}

	.usage__fill {
		display: block;
		height: 100%;
		min-width: 2px;
		border-radius: inherit;
	}

	.usage__fill--accent {
		background: var(--accent);
	}

	.usage__fill--warning {
		background: var(--warning);
	}

	.usage__fill--danger {
		background: var(--danger);
	}
</style>
