<script lang="ts">
	import { can, session } from '#lib/session.svelte.js';
	import { projectNames } from '#lib/projectNames.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import AuditLog from '#lib/features/AuditLog.svelte';

	let projects = $state<string[]>([]);
	$effect(() => {
		if (can.admin()) projectNames().then((p) => (projects = p));
		else projects = [...(session.me?.managedProjects ?? [])].sort();
	});
</script>

<svelte:head><title>Audit log · doreg</title></svelte:head>

<PageHeader
	title="Audit log"
	description={can.admin()
		? 'Who did what, across the whole registry.'
		: 'Who did what in the projects you manage.'}
/>

<AuditLog {projects} />
