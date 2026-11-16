<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import { session } from '#lib/session.svelte.js';
	import { fail } from '#lib/toast.svelte.js';
	import Icon from './Icon.svelte';

	let open = $state(false);
	let root = $state<HTMLElement>();

	const me = $derived(session.me);

	async function logout() {
		open = false;
		try {
			await api.post('/api/logout');
		} catch (e) {
			fail(e);
			return;
		}
		session.me = null;
		await goto('/login');
	}

	function onWindowClick(e: MouseEvent) {
		if (open && root && !root.contains(e.target as Node)) open = false;
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Escape') open = false;
	}
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKey} />

{#if me}
	<div class="am" bind:this={root}>
		{#if open}
			<div class="am__menu" role="menu">
				<div class="am__head">
					<span class="am__name">{me.user.username}</span>
					<span class="am__role">{me.user.role}</span>
				</div>
				<a href="/account" role="menuitem" onclick={() => (open = false)}>
					<Icon name="user" size={15} /> Account
				</a>
				<button type="button" role="menuitem" onclick={logout}>
					<Icon name="logout" size={15} /> Sign out
				</button>
				<div class="am__version">doreg {me.version}</div>
			</div>
		{/if}
		<button
			type="button"
			class="am__trigger"
			aria-haspopup="menu"
			aria-expanded={open}
			onclick={() => (open = !open)}
		>
			<span class="am__avatar" aria-hidden="true">{me.user.username.slice(0, 1).toUpperCase()}</span
			>
			<span class="am__who">
				<span class="am__name">{me.user.username}</span>
				<span class="am__role">{me.user.role}</span>
			</span>
			<span class="am__chev"><Icon name="chevronsUpDown" size={14} /></span>
		</button>
	</div>
{/if}

<style>
	.am {
		position: relative;
	}

	.am__trigger {
		display: flex;
		align-items: center;
		gap: 0.625rem;
		width: 100%;
		height: 2.75rem;
		padding: 0 var(--space-2);
		border: 0;
		border-radius: var(--radius);
		background: none;
		color: var(--fg-subtle);
		text-align: left;
		cursor: pointer;
		transition:
			color var(--dur-micro) var(--ease),
			background-color var(--dur-micro) var(--ease);
	}

	.am__trigger:hover,
	.am__trigger[aria-expanded='true'] {
		background: var(--surface-hover);
		color: var(--fg-muted);
	}

	.am__avatar {
		display: grid;
		place-items: center;
		flex: none;
		width: 1.75rem;
		height: 1.75rem;
		border-radius: var(--radius);
		background: var(--avatar);
		color: var(--avatar-fg);
		font-size: var(--text-sm);
		font-weight: 600;
	}

	.am__who {
		display: grid;
		flex: 1;
		min-width: 0;
		line-height: 1.25;
	}

	.am__name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		color: var(--fg);
		font-size: var(--text-sm);
		font-weight: 500;
	}

	.am__role {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
		text-transform: capitalize;
	}

	.am__chev {
		display: grid;
		flex: none;
	}

	.am__menu {
		position: absolute;
		left: 0;
		right: 0;
		bottom: calc(100% + var(--space-2));
		z-index: 20;
		display: grid;
		gap: 1px;
		padding: var(--space-1);
		border-radius: var(--radius-xl);
		background: var(--surface-raised);
		box-shadow: var(--shadow-pop);
		animation: pop var(--dur-short) var(--ease);
	}

	@keyframes pop {
		from {
			opacity: 0;
			transform: translateY(4px) scale(0.98);
		}
	}

	.am__head {
		display: grid;
		padding: var(--space-2);
		margin-bottom: var(--space-1);
		border-bottom: 1px solid var(--border-strong);
		line-height: 1.35;
	}

	.am__menu a,
	.am__menu button {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		height: 2rem;
		padding: 0 var(--space-2);
		border: 0;
		border-radius: var(--radius);
		background: none;
		color: var(--fg-muted);
		font-size: var(--text-sm);
		text-align: left;
		cursor: pointer;
		transition:
			color var(--dur-micro) var(--ease),
			background-color var(--dur-micro) var(--ease);
	}

	.am__menu a:hover,
	.am__menu button:hover {
		background: var(--surface-active);
		color: var(--fg);
		text-decoration: none;
	}

	.am__menu :focus-visible {
		outline-offset: -2px;
	}

	.am__version {
		padding: var(--space-2) var(--space-2) var(--space-1);
		margin-top: var(--space-1);
		border-top: 1px solid var(--border-strong);
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}
</style>
