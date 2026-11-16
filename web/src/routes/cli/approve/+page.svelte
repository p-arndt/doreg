<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError, q } from '#lib/api.js';
	import { normalizeUserCode, when } from '#lib/format.js';
	import { session } from '#lib/session.svelte.js';
	import Button from '#lib/components/Button.svelte';
	import Field from '#lib/components/Field.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Input from '#lib/components/Input.svelte';
	import Logo from '#lib/components/Logo.svelte';
	import Notice from '#lib/components/Notice.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { DeviceRequest } from '#lib/types.js';

	const code = $derived(normalizeUserCode(page.url.searchParams.get('code') ?? ''));

	let request = $state<DeviceRequest | null>(null);
	let error = $state('');
	let busy = $state<'' | 'approve' | 'deny'>('');
	let typed = $state('');

	$effect(() => {
		const c = code;
		request = null;
		error = '';
		if (!c) return;
		api.get<DeviceRequest>(`/api/cli/device/${q(c)}`).then(
			(r) => (request = r),
			(e) =>
				(error =
					e instanceof ApiError && e.status === 404
						? 'This code is unknown. Check it against your terminal, or run doreg login again.'
						: e instanceof Error
							? e.message
							: String(e))
		);
	});

	// A pending request whose clock ran out is expired even before the server says so.
	const expired = $derived(
		!!request &&
			(request.status === 'expired' ||
				(request.status === 'pending' && request.expiresAt < Date.now() / 1000))
	);

	async function decide(action: 'approve' | 'deny') {
		if (!request) return;
		busy = action;
		try {
			await api.post(`/api/cli/device/${q(request.userCode)}/${action}`);
			request = { ...request, status: action === 'approve' ? 'approved' : 'denied' };
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			busy = '';
		}
	}

	function submitCode(e: SubmitEvent) {
		e.preventDefault();
		const c = normalizeUserCode(typed);
		if (c.length === 9) goto(`?code=${encodeURIComponent(c)}`, { replace: true });
	}
</script>

<svelte:head><title>Approve command-line sign-in · doreg</title></svelte:head>

<main class="approve">
	<div class="card">
		<span class="brand"><Logo size={22} /> doreg</span>

		{#if !code}
			<div class="head">
				<h1>Sign in to doreg from the command line</h1>
				<p class="muted">Enter the code your terminal shows after <code>doreg login</code>.</p>
			</div>
			<form class="code-form" onsubmit={submitCode}>
				<Field label="Code">
					<Input
						bind:value={typed}
						oninput={() => (typed = normalizeUserCode(typed))}
						placeholder="BCDF-GHJK"
						autocomplete="off"
						spellcheck={false}
						mono
						required
					/>
				</Field>
				<Button variant="primary" type="submit" disabled={normalizeUserCode(typed).length !== 9}
					>Continue</Button
				>
			</form>
		{:else if error && !request}
			<div class="state state--bad" role="alert">
				<Icon name="alert" size={18} />
				<div>
					<h1>Can’t use this code</h1>
					<p class="muted">Code <code>{code}</code>. {error}</p>
				</div>
			</div>
			<Button href="/cli/approve">Enter a different code</Button>
		{:else if !request}
			<Skeleton rows={3} columns={[1]} />
		{:else if request.status === 'approved'}
			<div class="state state--ok" role="status">
				<Icon name="check" size={18} />
				<div>
					<h1>You’re signed in</h1>
					<p class="muted">You can close this tab and return to the terminal.</p>
				</div>
			</div>
		{:else if request.status === 'denied'}
			<div class="state" role="status">
				<Icon name="x" size={18} />
				<div>
					<h1>Request denied</h1>
					<p class="muted">The terminal was not signed in. You can close this tab.</p>
				</div>
			</div>
		{:else if expired}
			<div class="state state--bad" role="alert">
				<Icon name="clock" size={18} />
				<div>
					<h1>This code has expired</h1>
					<p class="muted">
						Code <code>{request.userCode}</code> ran out. Run <code>doreg login</code> again to get a
						new one.
					</p>
				</div>
			</div>
		{:else}
			<div class="head">
				<h1>Sign in to doreg from the command line?</h1>
				<p class="muted">
					Check that this code matches the one in your terminal before you approve.
				</p>
			</div>

			<div class="code-pill" aria-label="Code">{request.userCode}</div>

			<dl class="details">
				<div>
					<dt>Device</dt>
					<dd>{request.client || 'Unknown'}</dd>
				</div>
				<div>
					<dt>Account</dt>
					<dd>{session.me?.user.username}</dd>
				</div>
				<div>
					<dt>Expires</dt>
					<dd title={when(request.expiresAt)}><Time unix={request.expiresAt} /></dd>
				</div>
			</dl>

			{#if error}<Notice tone="danger">{error}</Notice>{/if}

			<div class="actions">
				<Button
					variant="primary"
					loading={busy === 'approve'}
					disabled={!!busy}
					onclick={() => decide('approve')}>Approve</Button
				>
				<Button loading={busy === 'deny'} disabled={!!busy} onclick={() => decide('deny')}
					>Deny</Button
				>
			</div>
			<p class="fine">
				Approving gives this device access to the registry as you, until you revoke it under
				Account.
			</p>
		{/if}
	</div>
</main>

<style>
	.approve {
		display: grid;
		place-items: center;
		min-height: 100dvh;
		padding: var(--space-5) var(--space-4);
		background: var(--bg-sidebar);
	}

	.card {
		display: grid;
		gap: var(--space-5);
		width: 100%;
		max-width: 26rem;
		padding: var(--space-6);
		border: 1px solid var(--border);
		border-radius: var(--radius-xl, 12px);
		background: var(--surface);
		box-shadow: var(--shadow-pop);
	}

	.brand {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		font-weight: 600;
		letter-spacing: -0.02em;
	}

	.head {
		display: grid;
		gap: var(--space-2);
	}

	h1 {
		font-size: var(--text-lg);
		line-height: 1.25;
		text-wrap: balance;
		overflow-wrap: break-word;
		min-width: 0;
	}

	.head p,
	.state p {
		font-size: var(--text-sm);
	}

	/* The code is what the person compares with the terminal, so it is the largest thing on the card. */
	.code-pill {
		justify-self: start;
		padding: var(--space-3) var(--space-5);
		border: 1px solid var(--accent-line);
		border-radius: var(--radius-lg);
		background: var(--bg);
		color: var(--fg);
		font-family: var(--font-mono);
		font-size: 1.75rem;
		font-weight: 500;
		letter-spacing: 0.12em;
	}

	.details {
		display: grid;
		margin: 0;
		border-top: 1px solid var(--border);
	}

	.details > div {
		display: flex;
		justify-content: space-between;
		gap: var(--space-4);
		padding: var(--space-2) 0;
		border-bottom: 1px solid var(--border);
		font-size: var(--text-sm);
	}

	dt {
		color: var(--fg-muted);
	}

	dd {
		margin: 0;
		text-align: right;
		overflow-wrap: anywhere;
	}

	.actions {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-2);
	}

	.actions :global(.btn) {
		height: 2.375rem;
	}

	.fine {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	.code-form {
		display: grid;
		gap: var(--space-3);
	}

	.code-form :global(input) {
		font-size: var(--text-lg);
	}

	.state {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
	}

	.state > :global(.icon) {
		flex: none;
		margin-top: 0.2rem;
		color: var(--fg-muted);
	}

	.state--ok > :global(.icon) {
		color: var(--accent);
	}

	.state--bad > :global(.icon) {
		color: var(--danger);
	}

	.state div {
		display: grid;
		gap: var(--space-1);
	}
</style>
