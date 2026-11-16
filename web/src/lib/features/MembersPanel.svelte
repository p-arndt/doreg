<script lang="ts">
	import { api, q } from '#lib/api.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Input from '#lib/components/Input.svelte';
	import Select from '#lib/components/Select.svelte';
	import ConfirmButton from '#lib/components/ConfirmButton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { Member } from '#lib/types.js';

	type MemberRole = Member['role'];

	let {
		project,
		members,
		onchange
	}: { project: string; members: Member[]; onchange: () => Promise<void> | void } = $props();

	const roles: { value: MemberRole; label: string; hint: string }[] = [
		{ value: 'read', label: 'Read', hint: 'Pull images' },
		{ value: 'write', label: 'Write', hint: 'Pull, push and delete images' },
		{ value: 'admin', label: 'Admin', hint: 'Also members, robots, webhooks and settings' }
	];

	let kind = $state<'user' | 'group'>('user');
	let subject = $state('');
	let role = $state<MemberRole>('write');
	let busy = $state(false);

	const base = $derived(`/api/projects/${q(project)}/members`);

	async function add(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		try {
			await api.post(
				base,
				kind === 'user' ? { username: subject, role } : { group: subject, role }
			);
			notify(`Added ${subject} as ${role}.`);
			subject = '';
			await onchange();
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}

	async function setRole(m: Member, r: MemberRole) {
		try {
			await api.put(`${base}/${m.id}`, { role: r });
			notify(`${m.username ?? m.group} is now ${r}.`);
		} catch (e) {
			fail(e);
		}
		await onchange();
	}

	async function remove(m: Member) {
		try {
			await api.del(`${base}/${m.id}`);
			await onchange();
		} catch (e) {
			fail(e);
		}
	}
</script>

<form class="add" onsubmit={add}>
	<Select
		bind:value={kind}
		aria-label="Member type"
		options={[
			{ value: 'user', label: 'User' },
			{ value: 'group', label: 'SSO group' }
		]}
	/>
	<Input
		bind:value={subject}
		required
		placeholder={kind === 'user' ? 'Username' : 'Group name from your identity provider'}
		aria-label={kind === 'user' ? 'Username' : 'Group name'}
		class="add__subject"
	/>
	<Select bind:value={role} aria-label="Role" options={roles} />
	<Button variant="primary" type="submit" icon="plus" loading={busy}>Add</Button>
</form>
<p class="hint">{roles.find((r) => r.value === role)?.hint}.</p>

{#if members.length === 0}
	<EmptyState
		icon="users"
		title="No members"
		description="Only administrators can reach this project. Add users or SSO groups to give them access."
	/>
{:else}
	<Table label="Members">
		<thead>
			<tr>
				<th>Member</th>
				<th>Role</th>
				<th class="col-added">Added</th>
				<th class="shrink"><span class="visually-hidden">Actions</span></th>
			</tr>
		</thead>
		<tbody>
			{#each members as m (m.id)}
				<tr>
					<td>
						<span class="who">
							{m.username ?? m.group}
							{#if m.group}<Badge title="Everyone in this SSO group">Group</Badge>{/if}
						</span>
					</td>
					<td>
						<Select
							size="sm"
							aria-label={`Role of ${m.username ?? m.group}`}
							value={m.role}
							options={roles}
							onchange={(v) => setRole(m, v)}
						/>
					</td>
					<td class="muted nowrap col-added"><Time unix={m.createdAt} /></td>
					<td class="shrink">
						<span class="row-actions">
							<ConfirmButton
								label={`Remove ${m.username ?? m.group}`}
								iconOnly
								icon="trash"
								question="Remove?"
								confirmLabel="Remove"
								onconfirm={() => remove(m)}
							/>
						</span>
					</td>
				</tr>
			{/each}
		</tbody>
	</Table>
{/if}

<style>
	.add {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}

	.add :global(.add__subject) {
		flex: 1 1 14rem;
		max-width: 22rem;
	}

	.hint {
		margin: var(--space-2) 0 var(--space-5);
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	.who {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		font-weight: 500;
	}

	td :global(.select) {
		min-width: 6.5rem;
	}

	@media (max-width: 40rem) {
		.col-added {
			display: none;
		}
	}
</style>
