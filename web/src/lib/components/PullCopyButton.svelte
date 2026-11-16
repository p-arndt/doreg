<script lang="ts">
	import { pullText } from '#lib/format.js';
	import { prefs } from '#lib/prefs.svelte.js';
	import CopyButton from './CopyButton.svelte';

	// Row quick action: copies an image as reference or pull command, per the user's preference.
	let { reference, platform }: { reference: string; platform?: string } = $props();
</script>

<CopyButton
	value={pullText(reference, prefs.copyMode, platform)}
	label={prefs.copyMode === 'ref'
		? `Copy ${reference}`
		: `Copy docker pull${platform ? ` --platform ${platform}` : ''}`}
/>
