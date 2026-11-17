<script lang="ts">
	import { pullText, splitReference } from '#lib/format.js';
	import { prefs } from '#lib/prefs.svelte.js';
	import CopyButton from './CopyButton.svelte';

	// The pullable reference of an image, shown and copied as the bare reference or the full
	// `docker pull` command. Which one is the user's copy preference, set on the account page.
	let { reference }: { reference: string } = $props();
	const text = $derived(pullText(reference, prefs.copyMode));
	const parts = $derived(splitReference(reference));
</script>

<div class="pull">
	{#if prefs.copyMode === 'command'}<span class="pull__prompt" aria-hidden="true">$</span>{/if}
	<code title={text}>
		{#if prefs.copyMode === 'command'}<span class="pull__cmd">docker pull</span>{/if}<span
			class="pull__head">{parts.head}</span
		><span class="pull__tail">{parts.tail}</span>
	</code>
	<CopyButton
		value={text}
		label={prefs.copyMode === 'ref' ? 'Copy image reference' : 'Copy pull command'}
	/>
</div>

<style>
	.pull {
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
		box-shadow: var(--shadow-control);
	}

	.pull__prompt {
		color: var(--fg-subtle);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
	}

	code {
		display: flex;
		flex: 1;
		min-width: 0;
		padding: 0;
		background: none;
		color: var(--fg-muted);
		font-size: var(--text-xs);
		white-space: nowrap;
	}

	code > span {
		overflow: hidden;
		text-overflow: ellipsis;
	}

	/* When space runs out, the command prefix and the host go first: name and tag are what
	   tells two references apart. */
	.pull__cmd {
		flex: 0 100000 auto;
		min-width: 0;
		margin-right: 1ch;
		color: var(--fg-subtle);
	}

	.pull__head {
		flex: 0 10000 auto;
		min-width: 2ch;
	}

	.pull__head:empty {
		min-width: 0;
	}

	.pull__tail {
		flex: 0 1 auto;
		min-width: 4ch;
		color: var(--fg);
	}

	.pull > :global(.btn) {
		height: 1.625rem;
		width: 1.625rem;
		border-radius: var(--radius-xs);
	}

	/* On a phone the reference is the point of the block: wrap it instead of cutting it off. */
	@media (max-width: 30rem) {
		.pull {
			height: auto;
			min-height: var(--control-h);
			padding-block: var(--space-1);
		}

		code {
			display: block;
			overflow-wrap: anywhere;
			white-space: normal;
		}
	}
</style>
