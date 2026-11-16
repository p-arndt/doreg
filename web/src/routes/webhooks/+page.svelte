<script lang="ts">
	import { can } from '#lib/session.svelte.js';
	import { projectNames } from '#lib/projectNames.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import WebhooksPanel from '#lib/features/WebhooksPanel.svelte';

	let projects = $state<string[]>([]);
	projectNames().then((p) => (projects = p));
</script>

<svelte:head><title>Webhooks · doreg</title></svelte:head>

<PageHeader
	title="Webhooks"
	description="Every webhook. Project admins manage their own under each project."
/>

{#if can.admin()}
	<WebhooksPanel {projects} />
{:else}
	<ErrorState
		error="Only administrators see all webhooks. Open a project to manage its webhooks."
	/>
{/if}
