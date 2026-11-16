<script lang="ts">
	import { api } from '#lib/api.js';
	import { session } from '#lib/session.svelte.js';
	import { prefs, setCopyMode } from '#lib/prefs.svelte.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import { shellQuote } from '#lib/format.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Section from '#lib/components/Section.svelte';
	import Field from '#lib/components/Field.svelte';
	import Input from '#lib/components/Input.svelte';
	import Button from '#lib/components/Button.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Command from '#lib/components/Command.svelte';
	import ConfirmButton from '#lib/components/ConfirmButton.svelte';
	import SecretReveal from '#lib/components/SecretReveal.svelte';
	import ChoiceCards from '#lib/components/ChoiceCards.svelte';
	import Table from '#lib/components/Table.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { CliToken, Me } from '#lib/types.js';

	let current = $state('');
	let next = $state('');
	let busy = $state(false);
	let creating = $state(false);
	let secret = $state<{ username: string; secret: string } | null>(null);

	const host = location.host;
	const user = $derived(session.me?.user);
	const usesPassword = $derived(!user?.sso || !!user?.hasPassword);

	let tokens = $state<CliToken[] | null>(null);
	let tokensError = $state<unknown>(null);

	async function loadTokens() {
		tokensError = null;
		try {
			tokens = await api.get<CliToken[]>('/api/me/tokens');
		} catch (e) {
			tokensError = e;
		}
	}
	loadTokens();

	async function revokeToken(t: CliToken) {
		try {
			await api.del(`/api/me/tokens/${t.id}`);
			notify(`Signed out ${t.name}.`);
			await loadTokens();
		} catch (e) {
			fail(e);
		}
	}

	async function refreshMe() {
		session.me = await api.get<Me>('/api/me');
	}

	async function changePassword(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		try {
			await api.put('/api/me/password', { current, new: next });
			notify('Password changed. Run docker login again on your machines.');
			current = next = '';
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}

	async function createSecret() {
		creating = true;
		try {
			secret = await api.post<{ username: string; secret: string }>('/api/me/cli-secret');
			await refreshMe();
		} catch (e) {
			fail(e);
		} finally {
			creating = false;
		}
	}

	async function deleteSecret() {
		try {
			await api.del('/api/me/cli-secret');
			secret = null;
			notify('CLI secret revoked. docker logins that used it stop working.');
			await refreshMe();
		} catch (e) {
			fail(e);
		}
	}
</script>

<svelte:head><title>Account · doreg</title></svelte:head>

<PageHeader title="Account" description="Your sign-in for the web interface and docker login.">
	{#snippet meta()}
		<span class="who">{user?.username}</span>
		<Badge tone={user?.role === 'admin' ? 'accent' : 'neutral'}>{user?.role}</Badge>
		{#if user?.sso}<Badge>SSO</Badge>{/if}
		{#if user?.email}<span>{user.email}</span>{/if}
		<span class="subtle">doreg {session.me?.version}</span>
	{/snippet}
</PageHeader>

<div class="layout">
	<Section
		title="Command-line logins"
		description="Run this on a machine with a browser: it opens doreg, you approve, and docker is signed in."
	>
		<Command command={`doreg login ${host}`} />
		<div class="tokens">
			{#if tokensError}
				<ErrorState error={tokensError} retry={loadTokens} />
			{:else if !tokens}
				<Skeleton rows={2} columns={[3, 2, 2, 1]} />
			{:else if tokens.length === 0}
				<EmptyState
					icon="terminal"
					title="No command-line logins"
					description="Machines you sign in with doreg login show up here, and you can sign them out again."
				/>
			{:else}
				<Table label="Command-line logins">
					<thead>
						<tr>
							<th>Device</th>
							<th class="col-created">Signed in</th>
							<th>Last used</th>
							<th class="col-expires">Expires</th>
							<th class="shrink"><span class="visually-hidden">Actions</span></th>
						</tr>
					</thead>
					<tbody>
						{#each tokens as t (t.id)}
							<tr>
								<td class="token-name">{t.name}</td>
								<td class="muted nowrap col-created"><Time unix={t.createdAt} /></td>
								<td class="muted nowrap"><Time unix={t.lastUsedAt} /></td>
								<td class="muted nowrap col-expires">
									{#if t.expiresAt}<Time unix={t.expiresAt} />{:else}never{/if}
								</td>
								<td class="shrink">
									<span class="row-actions">
										<ConfirmButton
											label={`Revoke ${t.name}`}
											iconOnly
											icon="trash"
											question="Sign out?"
											confirmLabel="Revoke"
											onconfirm={() => revokeToken(t)}
										/>
									</span>
								</td>
							</tr>
						{/each}
					</tbody>
				</Table>
			{/if}
		</div>
	</Section>

	<Section
		title="CLI secret"
		description={user?.sso
			? 'The fallback for machines without a browser, like servers and CI: use the secret as the password for docker login.'
			: 'The fallback for machines without a browser, like servers and CI: a separate password for docker login, so your account password stays out of scripts.'}
	>
		{#if secret}
			<SecretReveal
				title="Your new CLI secret"
				secret={secret.secret}
				login={`docker login ${host} -u ${shellQuote(secret.username)}`}
				ondismiss={() => (secret = null)}
			/>
		{:else}
			<div class="card">
				<div class="card__text">
					{#if user?.hasCliSecret}
						<Badge tone="success">Active</Badge>
						<span class="muted">Creating a new one replaces it.</span>
					{:else}
						<span class="muted">No CLI secret yet.</span>
					{/if}
				</div>
				<div class="row">
					<Button
						variant={user?.hasCliSecret ? 'secondary' : 'primary'}
						icon="key"
						loading={creating}
						onclick={createSecret}>{user?.hasCliSecret ? 'Regenerate' : 'Create secret'}</Button
					>
					{#if user?.hasCliSecret}
						<ConfirmButton label="Revoke" size="md" question="Revoke?" onconfirm={deleteSecret} />
					{/if}
				</div>
			</div>
		{/if}
		{#if (usesPassword || user?.hasCliSecret) && !secret}
			<div class="login-line">
				<Command command={`docker login ${host} -u ${shellQuote(user?.username ?? '')}`} />
			</div>
		{/if}
	</Section>

	<Section
		title="Copy buttons"
		description="What the copy buttons next to images put on the clipboard. Saved in this browser."
	>
		<ChoiceCards
			label="Copy images as"
			value={prefs.copyMode}
			onchange={setCopyMode}
			options={[
				{
					value: 'ref',
					title: 'Image reference',
					description: 'For compose files, Kubernetes manifests and FROM lines.',
					preview: `${host}/team/api:1.0`
				},
				{
					value: 'command',
					title: 'Pull command',
					description: 'Ready to paste into a terminal.',
					preview: `docker pull ${host}/team/api:1.0`
				}
			]}
		/>
	</Section>

	{#if usesPassword}
		<Section title="Change password" description="Existing docker logins stop working afterwards.">
			<form class="form" onsubmit={changePassword}>
				<Field label="Current password">
					<Input type="password" required autocomplete="current-password" bind:value={current} />
				</Field>
				<Field label="New password" hint="At least 8 characters.">
					<Input
						type="password"
						minlength={8}
						required
						autocomplete="new-password"
						bind:value={next}
					/>
				</Field>
				<div>
					<Button variant="primary" type="submit" loading={busy}>Change password</Button>
				</div>
			</form>
		</Section>
	{/if}
</div>

<style>
	.layout {
		max-width: 44rem;
	}

	.tokens {
		margin-top: var(--space-4);
	}

	.token-name {
		font-weight: 500;
	}

	@media (max-width: 40rem) {
		.col-created,
		.col-expires {
			display: none;
		}
	}

	.who {
		color: var(--fg);
		font-weight: 500;
	}

	.card {
		display: flex;
		align-items: center;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: var(--space-3);
		padding: var(--space-4);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
	}

	.card__text {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-sm);
	}

	.login-line {
		margin-top: var(--space-3);
	}

	.form {
		display: grid;
		gap: var(--space-4);
	}
</style>
