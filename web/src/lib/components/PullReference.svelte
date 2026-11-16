<script lang="ts">
	import { pullText } from '#lib/format.js';
	import { prefs, setCopyMode } from '#lib/prefs.svelte.js';
	import CopyButton from './CopyButton.svelte';
	import SegmentedControl from './SegmentedControl.svelte';

	// The pullable reference of an image, shown and copied as the bare reference or the full
	// `docker pull` command, following the user's copy preference (switchable right here).
	let { reference }: { reference: string } = $props();
	const text = $derived(pullText(reference, prefs.copyMode));
</script>

<div class="pull">
	<div class="pull__line">
		<code title={text}>{text}</code>
		<CopyButton
			value={text}
			label={prefs.copyMode === 'ref' ? 'Copy image reference' : 'Copy pull command'}
		/>
	</div>
	<SegmentedControl
		label="Copy as"
		value={prefs.copyMode}
		onchange={setCopyMode}
		options={[
			{ value: 'ref', label: 'Reference', title: 'Copy buttons copy host/repo:tag' },
			{ value: 'command', label: 'docker pull', title: 'Copy buttons copy the full pull command' }
		]}
	/>
</div>

<style>
	.pull {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
		max-width: 100%;
	}

	.pull__line {
		display: flex;
		align-items: center;
		flex: 1;
		gap: var(--space-2);
		min-width: 0;
		height: var(--control-h);
		padding: 0 0.125rem 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg-sidebar);
	}

	code {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		padding: 0;
		background: none;
		color: var(--fg-muted);
		font-size: var(--text-xs);
	}

	.pull__line :global(.btn) {
		height: 1.625rem;
		width: 1.625rem;
	}

	@media (max-width: 30rem) {
		.pull {
			flex-wrap: wrap;
		}
	}
</style>
