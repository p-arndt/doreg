<script lang="ts">
	import { api } from '#lib/api.js';
	import { session } from '#lib/session.svelte.js';
	import { when } from '#lib/format.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Field from '#lib/components/Field.svelte';
	import Input from '#lib/components/Input.svelte';
	import Select from '#lib/components/Select.svelte';
	import ConfirmButton from '#lib/components/ConfirmButton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { Role, User } from '#lib/types.js';

	let users = $state<User[] | null>(null);
	let error = $state<unknown>(null);
	let form = $state({ username: '', password: '', role: 'user' as Role });
	let busy = $state(false);
	let resetFor = $state<number | null>(null);
	let resetPassword = $state('');

	const roles: { value: Role; label: string; hint: string }[] = [
		{ value: 'user', label: 'User', hint: 'Gets access through project membership.' },
		{ value: 'admin', label: 'Admin', hint: 'Everything: all projects, users and cleanup.' }
	];
	const roleHint = $derived(roles.find((r) => r.value === form.role)?.hint ?? '');

	async function load() {
		error = null;
		try {
			users = await api.get<User[]>('/api/users');
		} catch (e) {
			error = e;
		}
	}
	load();

	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		try {
			await api.post('/api/users', form);
			notify(`Created ${form.username}.`);
			form = { username: '', password: '', role: 'user' };
			await load();
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}

	async function setRole(u: User, role: Role) {
		try {
			await api.put(`/api/users/${u.id}`, { role });
			notify(`${u.username} is now ${role}.`);
			await load();
		} catch (e) {
			fail(e);
			await load();
		}
	}

	async function reset(e: SubmitEvent, u: User) {
		e.preventDefault();
		try {
			await api.put(`/api/users/${u.id}`, { role: u.role, password: resetPassword });
			notify(`Password changed for ${u.username}.`);
			resetFor = null;
			resetPassword = '';
		} catch (err) {
			fail(err);
		}
	}

	async function remove(u: User) {
		try {
			await api.del(`/api/users/${u.id}`);
			await load();
		} catch (e) {
			fail(e);
		}
	}
</script>

<svelte:head><title>Users · doreg</title></svelte:head>

<PageHeader
	title="Users"
	description="Accounts for the web interface and docker login. Project access is granted under Projects."
/>

<div class="layout">
	<div class="list">
		{#if error}
			<ErrorState {error} retry={load} />
		{:else if !users}
			<Skeleton rows={3} columns={[3, 2, 2, 1]} />
		{:else}
			<Table label="Users">
				<thead>
					<tr>
						<th>User</th>
						<th>Role</th>
						<th class="col-created">Last sign-in</th>
						<th class="shrink"><span class="visually-hidden">Actions</span></th>
					</tr>
				</thead>
				<tbody>
					{#each users as u (u.id)}
						<tr>
							<td>
								<span class="user">
									<span class="avatar" aria-hidden="true"
										>{u.username.slice(0, 1).toUpperCase()}</span
									>
									<span class="user__who">
										<span class="user__name">
											{u.username}
											{#if u.id === session.me?.user.id}<Badge tone="accent">You</Badge>{/if}
											{#if u.sso}<Badge title="Signs in with single sign-on">SSO</Badge>{/if}
										</span>
										{#if u.email}<span class="user__email">{u.email}</span>{/if}
									</span>
								</span>
							</td>
							<td>
								<Select
									size="sm"
									aria-label={`Role of ${u.username}`}
									value={u.role}
									onchange={(e) => setRole(u, e.currentTarget.value as Role)}
								>
									{#each roles as r (r.value)}<option value={r.value}>{r.label}</option>{/each}
								</Select>
							</td>
							<td class="muted nowrap col-created">
								{#if u.lastLoginAt}<Time unix={u.lastLoginAt} />{:else}<span
										class="subtle"
										title={`Created ${when(u.createdAt)}`}>never</span
									>{/if}
							</td>
							<td class="shrink">
								{#if resetFor === u.id}
									<form class="reset" onsubmit={(e) => reset(e, u)}>
										<Input
											size="sm"
											type="password"
											minlength={8}
											required
											autofocus
											placeholder="New password"
											aria-label={`New password for ${u.username}`}
											autocomplete="new-password"
											bind:value={resetPassword}
										/>
										<Button size="sm" variant="primary" type="submit">Set</Button>
										<Button size="sm" variant="ghost" onclick={() => (resetFor = null)}
											>Cancel</Button
										>
									</form>
								{:else}
									<span class="row-actions">
										{#if !u.sso || u.hasPassword}<Button
												size="sm"
												variant="ghost"
												icon="key"
												onclick={() => {
													resetFor = u.id;
													resetPassword = '';
												}}>Password</Button
											>{/if}
										{#if u.id !== session.me?.user.id}
											<ConfirmButton
												label={`Delete ${u.username}`}
												iconOnly
												icon="trash"
												question={`Delete ${u.username}?`}
												onconfirm={() => remove(u)}
											/>
										{/if}
									</span>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</Table>
		{/if}
	</div>

	<aside class="add">
		<form class="add__form" onsubmit={create}>
			<h2>Add user</h2>
			<Field label="Username">
				<Input bind:value={form.username} required autocomplete="off" />
			</Field>
			<Field label="Password">
				{#snippet hint()}At least 8 characters. CI uses it with <code
						>docker login --password-stdin</code
					>.{/snippet}
				<Input
					type="password"
					minlength={8}
					required
					bind:value={form.password}
					autocomplete="new-password"
				/>
			</Field>
			<Field label="Role" hint={roleHint}>
				<Select bind:value={form.role}>
					{#each roles as r (r.value)}<option value={r.value}>{r.label}</option>{/each}
				</Select>
			</Field>
			<div>
				<Button variant="primary" type="submit" icon="plus" loading={busy}>Add user</Button>
			</div>
		</form>
	</aside>
</div>

<style>
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 20rem;
		gap: var(--space-6);
		align-items: start;
	}

	.user {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
	}

	.avatar {
		display: grid;
		place-items: center;
		flex: none;
		width: 1.5rem;
		height: 1.5rem;
		border-radius: 0;
		background: var(--avatar);
		font-size: var(--text-xs);
		font-weight: 600;
	}

	.user__who {
		display: grid;
		line-height: 1.35;
	}

	.user__name {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		font-weight: 500;
	}

	.user__email {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	.list :global(td .select) {
		min-width: 6.5rem;
	}

	.reset {
		display: flex;
		align-items: center;
		gap: var(--space-1);
	}

	.reset :global(.input) {
		width: 10rem;
	}

	.add__form {
		display: grid;
		gap: var(--space-4);
		padding: var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
	}

	.add__form :global(.select) {
		width: 100%;
	}

	@media (max-width: 64rem) {
		.layout {
			grid-template-columns: 1fr;
		}

		.add {
			max-width: 28rem;
		}
	}

	@media (max-width: 40rem) {
		.col-created {
			display: none;
		}
	}
</style>
