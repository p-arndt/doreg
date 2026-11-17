<script lang="ts">
	import { envItems, runtimeItems } from '#lib/imageConfig.js';
	import type { ImageConfig } from '#lib/types.js';
	import Section from './Section.svelte';
	import DescriptionList from './DescriptionList.svelte';

	let { config }: { config: ImageConfig } = $props();
	const runtime = $derived(runtimeItems(config));
	const env = $derived(envItems(config));
	const labels = $derived(Object.entries(config.config?.Labels ?? {}));
</script>

<Section title="Runtime">
	<DescriptionList items={runtime} mono />
</Section>
{#if env.length}
	<Section title="Environment">
		<DescriptionList items={env} mono monoKeys />
	</Section>
{/if}
{#if labels.length}
	<Section title="Labels">
		<DescriptionList items={labels} mono monoKeys />
	</Section>
{/if}
