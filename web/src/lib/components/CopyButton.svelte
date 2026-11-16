<script lang="ts">
	import Button from './Button.svelte';
	import { fail } from '#lib/toast.svelte.js';

	// Icon-only by default; pass `text` as the visible label for a labelled button.
	let {
		value,
		label = 'Copy',
		text,
		size = 'sm'
	}: { value: string; label?: string; text?: string; size?: 'sm' | 'md' } = $props();
	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout>;

	async function copy() {
		try {
			await navigator.clipboard.writeText(value);
		} catch (e) {
			fail(e);
			return;
		}
		copied = true;
		clearTimeout(timer);
		timer = setTimeout(() => (copied = false), 1500);
	}
</script>

{#if text}
	<Button variant="ghost" {size} icon={copied ? 'check' : 'copy'} onclick={copy} title={label}
		>{copied ? 'Copied' : text}</Button
	>
{:else}
	<Button
		variant="ghost"
		{size}
		icon={copied ? 'check' : 'copy'}
		label={copied ? 'Copied' : label}
		onclick={copy}
	/>
{/if}
