<script lang="ts">
	let { size = 22 }: { size?: number } = $props();
</script>

<svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true"
	><rect width="32" height="32" rx="0" fill="var(--accent)" /><path
		d="M9 9.5h7.5a6.5 6.5 0 0 1 0 13H9z"
		fill="var(--fg-on-accent)"
	/><rect x="12.5" y="13" width="4" height="6" rx="1" fill="var(--accent)" /></svg
>
