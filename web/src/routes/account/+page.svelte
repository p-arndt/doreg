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
	import SegmentedControl from '#lib/components/SegmentedControl.svelte';
	import type { Me } from '#lib/types.js';

	let current = $state('');
	let next = $state('');
	let busy = $state(false);
	let creating = $state(false);
	let secret = $state<{ username: string; secret: string } | null>(null);

	const host = location.host;
	const user = $derived(session.me?.user);
	const usesPassword = $derived(!user?.sso || !!user?.hasPassword);

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
		title="CLI secret"
		description={user?.sso
			? 'You sign in here with single sign-on, which docker cannot do. Create a secret and use it as the password for docker login.'
			: 'A separate password for docker login, so your account password stays out of scripts and credential stores.'}
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
		description="What copy buttons next to images put on the clipboard. Stored in this browser."
	>
		<SegmentedControl
			label="Copy images as"
			value={prefs.copyMode}
			onchange={setCopyMode}
			options={[
				{ value: 'ref', label: `Reference: ${host}/team/api:1.0` },
				{ value: 'command', label: 'Command: docker pull …' }
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
		max-width: 36rem;
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
