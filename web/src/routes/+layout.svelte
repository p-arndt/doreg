<script lang="ts">
	import '@fontsource-variable/geist';
	import '@fontsource-variable/geist-mono';
	import '#lib/theme.css';
	import favicon from '#lib/assets/favicon.svg';
	import { page } from '$app/state';
	import { afterNavigate } from '$app/navigation';
	import { api } from '#lib/api.js';
	import { session } from '#lib/session.svelte.js';
	import { groupNav, isActive, visibleNav } from '#lib/nav.js';
	import Toaster from '#lib/components/Toaster.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Logo from '#lib/components/Logo.svelte';
	import AccountMenu from '#lib/components/AccountMenu.svelte';
	import Button from '#lib/components/Button.svelte';
	import type { Me } from '#lib/types.js';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
	let ready = $state(false);
	let menuOpen = $state(false);

	const isLogin = $derived(page.url.pathname === '/login');

	$effect(() => {
		if (isLogin || session.me) {
			ready = true;
			return;
		}
		api
			.get<Me>('/api/me')
			.then((me) => (session.me = me))
			.catch(() => {})
			.finally(() => (ready = true));
	});

	afterNavigate(() => (menuOpen = false));

	const nav = $derived(groupNav(visibleNav(session.me)));
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

{#if isLogin}
	{@render children()}
{:else if session.me}
	<div class="shell">
		<aside class="sidebar" class:is-open={menuOpen}>
			<div class="sidebar__top">
				<a class="brand" href="/" aria-label="doreg home">
					<Logo />
					<span>doreg</span>
				</a>
				<button
					type="button"
					class="sidebar__toggle"
					aria-label={menuOpen ? 'Close menu' : 'Open menu'}
					aria-expanded={menuOpen}
					aria-controls="main-nav"
					onclick={() => (menuOpen = !menuOpen)}
				>
					<Icon name={menuOpen ? 'x' : 'menu'} size={18} />
				</button>
			</div>
			<div class="sidebar__body" id="main-nav">
				<nav aria-label="Main">
					{#each nav as section (section.group)}
						<div class="nav-group">
							<p class="nav-group__label">{section.group}</p>
							{#each section.items as item (item.href)}
								<a
									href={item.href}
									class="nav-link"
									aria-current={isActive(item, page.url.pathname) ? 'page' : undefined}
								>
									<Icon name={item.icon} size={16} />
									{item.label}
								</a>
							{/each}
						</div>
					{/each}
				</nav>
				<div class="sidebar__foot">
					<AccountMenu />
				</div>
			</div>
		</aside>
		<main class="main">
			<div class="content">
				{@render children()}
			</div>
		</main>
	</div>
{:else if ready}
	<!-- /api/me failed with something other than 401 (which redirects). -->
	<main class="unreachable">
		<Logo size={32} />
		<h1>doreg is not reachable</h1>
		<p class="muted">The server did not answer. Check that it is running, then reload.</p>
		<Button onclick={() => location.reload()}>Reload</Button>
	</main>
{/if}
<Toaster />

<style>
	.shell {
		display: grid;
		grid-template-columns: 15rem minmax(0, 1fr);
		min-height: 100dvh;
	}

	.sidebar {
		position: sticky;
		top: 0;
		display: flex;
		flex-direction: column;
		height: 100dvh;
		padding: var(--space-3);
		border-right: 1px solid var(--border);
		background: var(--bg-sidebar);
	}

	.sidebar__top {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-1) var(--space-2) var(--space-5);
	}

	.brand {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--fg);
		font-size: 1.0625rem;
		font-weight: 700;
		letter-spacing: -0.03em;
	}

	.brand:hover {
		text-decoration: none;
	}

	.sidebar__toggle {
		display: none;
		place-items: center;
		width: 2rem;
		height: 2rem;
		border: 0;
		border-radius: var(--radius);
		background: none;
		color: var(--fg-muted);
		cursor: pointer;
	}

	.sidebar__toggle:hover {
		background: var(--surface-hover);
		color: var(--fg);
	}

	.sidebar__body {
		display: flex;
		flex: 1;
		flex-direction: column;
		min-height: 0;
	}

	nav {
		overflow-y: auto;
	}

	.nav-group {
		display: grid;
		gap: 1px;
	}

	/* Rim parts sections with a hairline above the head, never with a box. */
	.nav-group + .nav-group {
		margin-top: var(--space-4);
		padding-top: var(--space-3);
		border-top: 1px solid var(--border);
	}

	.nav-group__label {
		padding: 0 var(--space-2);
		margin-bottom: var(--space-1);
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--fg-subtle);
	}

	.nav-link {
		display: flex;
		align-items: center;
		gap: 0.625rem;
		height: 2rem;
		padding: 0 var(--space-2);
		border-radius: var(--radius);
		color: var(--fg-muted);
		font-weight: 500;
	}

	.nav-link :global(.icon) {
		color: var(--fg-subtle);
	}

	.nav-link:hover {
		background: var(--surface-hover);
		color: var(--fg);
		text-decoration: none;
	}

	.nav-link:active {
		background: var(--surface-active);
	}

	/* The current place: a lifted row and the lime square, the same mark a chosen table row gets. */
	.nav-link[aria-current='page'] {
		background: var(--surface);
		color: var(--fg);
	}

	.nav-link[aria-current='page']::after {
		content: '';
		width: 6px;
		height: 6px;
		margin-left: auto;
		background: var(--accent);
	}

	.nav-link[aria-current='page'] :global(.icon) {
		color: var(--fg);
	}

	.sidebar__foot {
		margin-top: auto;
		padding-top: var(--space-3);
	}

	.main {
		min-width: 0;
	}

	.content {
		max-width: 72rem;
		margin: 0 auto;
		padding: var(--space-6) var(--space-6) var(--space-7);
	}

	.unreachable {
		display: grid;
		justify-items: center;
		align-content: center;
		gap: var(--space-3);
		min-height: 100dvh;
		padding: var(--space-5);
		text-align: center;
	}

	.unreachable h1 {
		text-wrap: balance;
		font-size: var(--text-lg);
	}

	@media (max-width: 60rem) {
		.shell {
			grid-template-columns: 1fr;
			grid-template-rows: auto 1fr;
		}

		.sidebar {
			z-index: 30;
			height: auto;
			padding: var(--space-2) var(--space-3);
			border-right: 0;
			border-bottom: 1px solid var(--border);
		}

		.sidebar__top {
			padding: 0 0 0 var(--space-1);
		}

		.sidebar__toggle {
			display: grid;
		}

		.sidebar__body {
			display: none;
		}

		.sidebar.is-open .sidebar__body {
			display: flex;
			position: absolute;
			top: 100%;
			left: 0;
			right: 0;
			max-height: calc(100dvh - 3.25rem);
			padding: var(--space-2) var(--space-3) var(--space-3);
			border-bottom: 1px solid var(--border);
			background: var(--bg-sidebar);
			box-shadow: var(--shadow-drop);
		}

		.nav-link {
			height: 2.5rem;
		}

		.content {
			padding: var(--space-5) var(--space-4) var(--space-7);
		}
	}
</style>
