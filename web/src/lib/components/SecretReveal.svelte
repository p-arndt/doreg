<script lang="ts">
	import Button from './Button.svelte';
	import Command from './Command.svelte';
	import CopyButton from './CopyButton.svelte';
	import Icon from './Icon.svelte';

	// A freshly created secret, shown exactly once, with the docker login line that uses it.
	let {
		title,
		secret,
		login,
		ondismiss
	}: { title: string; secret: string; login: string; ondismiss: () => void } = $props();
</script>

<div class="secret" role="status">
	<div class="secret__head">
		<Icon name="key" size={16} />
		<div class="secret__text">
			<h3>{title}</h3>
			<p>Copy it now. doreg stores only a hash and cannot show it again.</p>
		</div>
		<Button variant="ghost" size="sm" icon="x" label="Done, hide the secret" onclick={ondismiss} />
	</div>
	<div class="secret__value">
		<code>{secret}</code>
		<CopyButton value={secret} label="Copy secret" />
	</div>
	<Command command={login} />
	<p class="secret__hint">Paste the secret when docker asks for the password.</p>
</div>

<style>
	.secret {
		display: grid;
		gap: var(--space-3);
		padding: var(--space-4);
		border: 1px solid var(--warning-line);
		border-radius: var(--radius-lg);
		background: var(--warning-soft);
	}

	.secret__head {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		color: var(--warning);
	}

	.secret__head > :global(.icon) {
		margin-top: 0.1875rem;
	}

	.secret__text {
		flex: 1;
		min-width: 0;
	}

	h3 {
		color: var(--fg);
	}

	.secret__text p,
	.secret__hint {
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.secret__value {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-1) var(--space-1) var(--space-1) var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius);
		background: var(--bg);
	}

	.secret__value code {
		flex: 1;
		min-width: 0;
		padding: 0;
		background: none;
		color: var(--fg);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
</style>
