<script lang="ts">
	let { size = 22 }: { size?: number } = $props();
</script>

<svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true"
	><rect width="32" height="32" rx="7.5" fill="var(--accent)" /><path
		d="M9 9.5h7.5a6.5 6.5 0 0 1 0 13H9z"
		fill="var(--fg-on-accent)"
	/><rect x="8" y="13" width="16" height="1.25" fill="var(--accent)" /><rect
		x="8"
		y="17.75"
		width="16"
		height="1.25"
		fill="var(--accent)"
	/></svg
>
