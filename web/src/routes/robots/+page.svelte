<script lang="ts">
	import { can } from '#lib/session.svelte.js';
	import { projectNames } from '#lib/projectNames.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import RobotsPanel from '#lib/features/RobotsPanel.svelte';

	let projects = $state<string[]>([]);
	projectNames().then((p) => (projects = p));
</script>

<svelte:head><title>Robot accounts · doreg</title></svelte:head>

<PageHeader
	title="Robot accounts"
	description="Every robot account. Project admins manage their own under each project."
/>

{#if can.admin()}
	<RobotsPanel {projects} />
{:else}
	<ErrorState
		error="Only administrators see all robot accounts. Open a project to manage its robots."
	/>
{/if}
