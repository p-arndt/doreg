<script lang="ts">
	import CopyButton from './CopyButton.svelte';

	// A shell command with a copy button, e.g. `docker pull host/repo:tag`.
	let { command }: { command: string } = $props();
</script>

<div class="cmd">
	<span class="cmd__prompt" aria-hidden="true">$</span>
	<code title={command}>{command}</code>
	<CopyButton value={command} label="Copy command" />
</div>

<style>
	.cmd {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
		max-width: 100%;
		height: var(--control-h);
		padding: 0 0.125rem 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg-sidebar);
	}

	.cmd__prompt {
		color: var(--fg-subtle);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
	}

	code {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		color: var(--fg-muted);
		font-size: var(--text-xs);
		background: none;
		padding: 0;
	}

	.cmd :global(.btn) {
		height: 1.625rem;
		width: 1.625rem;
	}
</style>
