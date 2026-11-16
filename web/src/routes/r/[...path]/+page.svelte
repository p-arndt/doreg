<script lang="ts">
	import { page } from '$app/state';
	import { parseRef } from '#lib/format.js';
	import RepoView from './RepoView.svelte';
	import ManifestView from './ManifestView.svelte';

	// "/r/team/app" is a repository, "/r/team/app:v1" and "/r/team/app@sha256:…" a manifest.
	const target = $derived(parseRef(page.params.path ?? ''));
</script>

{#key target.repo + '|' + target.ref}
	{#if target.ref}
		<ManifestView repo={target.repo} ref={target.ref} />
	{:else}
		<RepoView name={target.repo} />
	{/if}
{/key}
