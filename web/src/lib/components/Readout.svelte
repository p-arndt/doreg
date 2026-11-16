<script lang="ts">
	// A measured value: the number in ink, its unit at the same size in muted ("4.09 MB").
	let { value }: { value: string } = $props();
	const parts = $derived.by(() => {
		const i = value.lastIndexOf(' ');
		return i < 0 ? { n: value, unit: '' } : { n: value.slice(0, i), unit: value.slice(i + 1) };
	});
</script>

<span class="readout"
	>{parts.n}{#if parts.unit}<span class="readout__unit">{parts.unit}</span>{/if}</span
>

<style>
	.readout {
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}

	.readout__unit {
		margin-left: 0.25em;
		color: var(--fg-muted);
		font-weight: 400;
	}
</style>
