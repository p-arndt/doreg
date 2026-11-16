<script lang="ts" generics="T extends string">
	import Icon from './Icon.svelte';

	// Radio choices shown as cards, each with a title, a line of explanation and an optional
	// monospace preview of what the choice produces. Arrow keys move between them like any radio group.
	let {
		value,
		options,
		label,
		onchange
	}: {
		value: T;
		options: { value: T; title: string; description?: string; preview?: string }[];
		label: string;
		onchange: (v: T) => void;
	} = $props();

	const groupName = `choice-${Math.random().toString(36).slice(2, 8)}`;
</script>

<div class="choices" role="radiogroup" aria-label={label}>
	{#each options as o (o.value)}
		<label class="choice" class:is-on={o.value === value}>
			<input
				type="radio"
				name={groupName}
				value={o.value}
				checked={o.value === value}
				onchange={() => onchange(o.value)}
			/>
			<span class="choice__head">
				<span class="choice__title">{o.title}</span>
				<span class="choice__mark" aria-hidden="true">
					{#if o.value === value}<Icon name="check" size={12} />{/if}
				</span>
			</span>
			{#if o.description}<span class="choice__desc">{o.description}</span>{/if}
			{#if o.preview}<code class="choice__preview">{o.preview}</code>{/if}
		</label>
	{/each}
</div>

<style>
	.choices {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
		gap: var(--space-3);
	}

	.choice {
		position: relative;
		display: grid;
		gap: var(--space-2);
		align-content: start;
		padding: var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-lg);
		background: var(--surface);
		cursor: pointer;
		transition:
			border-color var(--dur-micro) var(--ease),
			background-color var(--dur-micro) var(--ease);
	}

	.choice:hover {
		border-color: var(--border-hover);
	}

	.choice.is-on {
		border-color: var(--accent-line);
		background: var(--row-selected, var(--accent-soft));
	}

	.choice:has(input:focus-visible) {
		outline: 2px solid var(--border-focus);
		outline-offset: 2px;
	}

	input {
		position: absolute;
		opacity: 0;
		pointer-events: none;
	}

	.choice__head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
	}

	.choice__title {
		font-weight: 600;
	}

	.choice__mark {
		display: grid;
		place-items: center;
		flex: none;
		width: 1.125rem;
		height: 1.125rem;
		border: 1px solid var(--border-hover);
		border-radius: 50%;
		color: var(--fg-on-accent);
	}

	.choice.is-on .choice__mark {
		border-color: var(--accent);
		background: var(--accent);
	}

	.choice__desc {
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.choice__preview {
		display: block;
		overflow: hidden;
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius);
		background: var(--bg);
		color: var(--fg-muted);
		font-size: var(--text-xs);
		text-overflow: ellipsis;
		white-space: nowrap;
	}
</style>
