<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api.js';
	import { session } from '#lib/session.svelte.js';
	import { safeNext } from '#lib/format.js';
	import Button from '#lib/components/Button.svelte';
	import Field from '#lib/components/Field.svelte';
	import Input from '#lib/components/Input.svelte';
	import Notice from '#lib/components/Notice.svelte';
	import Logo from '#lib/components/Logo.svelte';
	import type { AuthConfig, Me } from '#lib/types.js';

	let username = $state('');
	let password = $state('');
	// The OIDC callback redirects here with ?error= when single sign-on failed.
	let error = $state(page.url.searchParams.get('error') ?? '');
	let busy = $state(false);
	let auth = $state<AuthConfig | null>(null);

	const host = location.host;
	const next = $derived(safeNext(page.url.searchParams.get('next')));

	// Older servers have no /api/auth/config (404): password sign-in only.
	fetch('/api/auth/config', { credentials: 'same-origin' })
		.then((r) => (r.ok ? r.json() : null))
		.then((c: AuthConfig | null) => (auth = c))
		.catch(() => {});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			session.me = await api.post<Me>('/api/login', { username, password });
			await goto(next);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Sign-in failed.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Sign in · doreg</title></svelte:head>

<main class="login">
	<div class="login__card ticks">
		<div class="login__head">
			<span class="login__brand"><Logo size={22} /> doreg</span>
			<h1>Sign in</h1>
			<p class="muted">The account you use for <code>docker login {host}</code>.</p>
		</div>

		{#if error && auth?.localLogin === false}<Notice tone="danger">{error}</Notice>{/if}

		{#if auth?.oidc}
			<Button
				variant={auth.localLogin === false ? 'primary' : 'secondary'}
				href={`/api/auth/oidc/login?next=${encodeURIComponent(next)}`}
				reload
				icon="key"
				class="login__sso">Sign in with {auth.oidcName || 'SSO'}</Button
			>
			{#if auth.localLogin !== false}<div class="login__divider"><span>or</span></div>{/if}
		{/if}

		{#if auth?.localLogin !== false}<form onsubmit={submit}>
				<Field label="Username">
					<Input bind:value={username} autocomplete="username" required autofocus />
				</Field>
				<Field label="Password">
					<Input
						type="password"
						bind:value={password}
						autocomplete="current-password"
						required
						aria-invalid={error ? 'true' : undefined}
					/>
				</Field>
				{#if error}<Notice tone="danger">{error}</Notice>{/if}
				<Button variant="primary" type="submit" loading={busy} class="login__submit"
					>{busy ? 'Signing in…' : 'Sign in'}</Button
				>
			</form>{/if}
	</div>

	<p class="login__tagline">Self-hosted OCI registry</p>
</main>

<style>
	.login {
		display: grid;
		place-items: center;
		align-content: center;
		gap: var(--space-4);
		min-height: 100dvh;
		padding: var(--space-5) var(--space-4);
		background: var(--bg-sidebar);
	}

	.login__card {
		display: grid;
		gap: var(--space-5);
		width: 100%;
		max-width: 23rem;
		padding: var(--space-6);
		background: var(--surface);
	}

	.login__head {
		display: grid;
		gap: var(--space-2);
	}

	.login__brand {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		margin-bottom: var(--space-4);
		font-weight: 600;
		letter-spacing: -0.03em;
	}

	.login__head h1 {
		font-size: var(--text-xl);
		text-wrap: balance;
	}

	.login__head p {
		font-size: var(--text-sm);
	}

	.login__tagline {
		width: 100%;
		max-width: 23rem;
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	form {
		display: grid;
		gap: var(--space-4);
	}

	.login__divider {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	.login__divider::before,
	.login__divider::after {
		content: '';
		flex: 1;
		height: 1px;
		background: var(--border-strong);
	}

	.login__card :global(.login__sso),
	.login__card :global(.login__submit) {
		width: 100%;
		height: 2.375rem;
	}

	@media (max-width: 30rem) {
		.login__card {
			padding: var(--space-5);
		}
	}
</style>
