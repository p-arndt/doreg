<script lang="ts">
	import { api, q } from '#lib/api.js';
	import { dateToUnix, shellQuote, unixToDate, when } from '#lib/format.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Checkbox from '#lib/components/Checkbox.svelte';
	import Field from '#lib/components/Field.svelte';
	import Input from '#lib/components/Input.svelte';
	import Select from '#lib/components/Select.svelte';
	import ConfirmButton from '#lib/components/ConfirmButton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import SecretReveal from '#lib/components/SecretReveal.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { Robot, RobotWithSecret } from '#lib/types.js';

	// Robot accounts of one project, or (project unset) every robot for administrators,
	// who can also create system robots that reach all projects.
	let { project, projects = [] }: { project?: string; projects?: string[] } = $props();

	interface Draft {
		id: number;
		name: string;
		description: string;
		project: string;
		canPush: boolean;
		canDelete: boolean;
		expiry: 'never' | '30' | '90' | '365' | 'date';
		expiresOn: string;
		disabled: boolean;
	}

	let robots = $state<Robot[] | null>(null);
	let error = $state<unknown>(null);
	let draft = $state<Draft | null>(null);
	let saving = $state(false);
	let secret = $state<RobotWithSecret | null>(null);

	const host = location.host;
	const now = () => Date.now() / 1000;

	async function load() {
		error = null;
		try {
			robots = await api.get<Robot[]>(`/api/robots${project ? `?project=${q(project)}` : ''}`);
		} catch (e) {
			error = e;
		}
	}
	load();

	function newDraft() {
		secret = null;
		draft = {
			id: 0,
			name: '',
			description: '',
			project: project ?? '',
			canPush: true,
			canDelete: false,
			expiry: '365',
			expiresOn: '',
			disabled: false
		};
	}

	function editDraft(r: Robot) {
		secret = null;
		draft = {
			id: r.id,
			name: r.name,
			description: r.description,
			project: r.project ?? '',
			canPush: r.canPush,
			canDelete: r.canDelete,
			expiry: r.expiresAt ? 'date' : 'never',
			expiresOn: unixToDate(r.expiresAt),
			disabled: r.disabled
		};
	}

	function expiresAt(d: Draft): number {
		if (d.expiry === 'never') return 0;
		if (d.expiry === 'date') return dateToUnix(d.expiresOn);
		return Math.floor(now() + Number(d.expiry) * 86400);
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!draft) return;
		saving = true;
		const body = {
			description: draft.description,
			canPush: draft.canPush,
			canDelete: draft.canDelete,
			expiresAt: expiresAt(draft),
			disabled: draft.disabled
		};
		try {
			if (draft.id) {
				await api.put(`/api/robots/${draft.id}`, body);
				notify('Robot account saved.');
			} else {
				secret = await api.post<RobotWithSecret>('/api/robots', {
					...body,
					name: draft.name,
					project: draft.project
				});
			}
			draft = null;
			await load();
		} catch (err) {
			fail(err);
		} finally {
			saving = false;
		}
	}

	async function toggle(r: Robot) {
		try {
			await api.put(`/api/robots/${r.id}`, {
				description: r.description,
				canPush: r.canPush,
				canDelete: r.canDelete,
				expiresAt: r.expiresAt,
				disabled: !r.disabled
			});
			await load();
		} catch (e) {
			fail(e);
		}
	}

	async function rotate(r: Robot) {
		try {
			secret = await api.post<RobotWithSecret>(`/api/robots/${r.id}/secret`);
			draft = null;
			await load();
		} catch (e) {
			fail(e);
		}
	}

	async function remove(r: Robot) {
		try {
			await api.del(`/api/robots/${r.id}`);
			if (secret?.robot.id === r.id) secret = null;
			await load();
		} catch (e) {
			fail(e);
		}
	}

	const prefix = $derived(
		draft ? (draft.project ? `robot$${draft.project}+` : 'robot$') : 'robot$'
	);
</script>

<div class="head">
	<p class="lede">
		Non-human accounts for CI and deployments. Each has its own secret, permissions and expiry.
	</p>
	{#if !draft}
		<Button variant="primary" icon="plus" onclick={newDraft}>New robot account</Button>
	{/if}
</div>

{#if secret}
	<div class="block">
		<SecretReveal
			title={`Secret for ${secret.robot.name}`}
			secret={secret.secret}
			login={`docker login ${host} -u ${shellQuote(secret.robot.name)}`}
			ondismiss={() => (secret = null)}
		/>
	</div>
{/if}

{#if draft}
	<form class="editor block" onsubmit={save}>
		<h2>{draft.id ? `Edit ${draft.name}` : 'New robot account'}</h2>
		{#if !draft.id}
			<div class="grid2">
				{#if !project}
					<Field label="Scope" hint="System robots reach every project.">
						<Select bind:value={draft.project}>
							<option value="">System (all projects)</option>
							{#each projects as p (p)}<option value={p}>{p}</option>{/each}
						</Select>
					</Field>
				{/if}
				<Field label="Name" hint={`Signs in as ${prefix}${draft.name || 'name'}`}>
					<Input
						bind:value={draft.name}
						required
						pattern="[a-z0-9]+([._\-][a-z0-9]+)*"
						placeholder="ci"
						autocomplete="off"
					/>
				</Field>
			</div>
		{/if}
		<Field label="Description">
			<Input bind:value={draft.description} placeholder="GitHub Actions for team/api" />
		</Field>
		<div class="perms">
			<span class="perms__label">Permissions</span>
			<Checkbox label="Pull" checked disabled description="Always allowed." />
			<Checkbox label="Push" bind:checked={draft.canPush} />
			<Checkbox label="Delete" bind:checked={draft.canDelete} description="Tags and manifests." />
		</div>
		<div class="grid2">
			<Field label="Expires">
				<Select bind:value={draft.expiry}>
					<option value="30">In 30 days</option>
					<option value="90">In 90 days</option>
					<option value="365">In 1 year</option>
					<option value="date">On a date</option>
					<option value="never">Never</option>
				</Select>
			</Field>
			{#if draft.expiry === 'date'}
				<Field label="Expiry date">
					<Input type="date" bind:value={draft.expiresOn} required />
				</Field>
			{/if}
		</div>
		{#if draft.id}
			<Checkbox label="Disabled" bind:checked={draft.disabled} />
		{/if}
		<div class="row">
			<Button variant="primary" type="submit" loading={saving}
				>{draft.id ? 'Save' : 'Create and show secret'}</Button
			>
			<Button variant="ghost" onclick={() => (draft = null)}>Cancel</Button>
		</div>
	</form>
{/if}

{#if error}
	<ErrorState {error} retry={load} />
{:else if !robots}
	<Skeleton rows={3} columns={[3, 2, 2, 2, 1]} />
{:else if robots.length === 0}
	{#if !draft && !secret}
		<EmptyState
			icon="bot"
			title="No robot accounts"
			description="Create one for each pipeline or server that pulls or pushes images."
		/>
	{/if}
{:else}
	<Table label="Robot accounts">
		<thead>
			<tr>
				<th>Account</th>
				{#if !project}<th class="col-scope">Scope</th>{/if}
				<th>Permissions</th>
				<th class="col-expires">Expires</th>
				<th class="col-used">Last used</th>
				<th class="shrink"><span class="visually-hidden">Actions</span></th>
			</tr>
		</thead>
		<tbody>
			{#each robots as r (r.id)}
				{@const expired = r.expiresAt > 0 && r.expiresAt < now()}
				<tr class:is-off={r.disabled || expired}>
					<td>
						<div class="robot">
							<span class="robot__name">
								{r.name}
								{#if r.disabled}<Badge>Disabled</Badge>{:else if expired}<Badge tone="danger"
										>Expired</Badge
									>{/if}
							</span>
							{#if r.description}<span class="robot__desc">{r.description}</span>{/if}
						</div>
					</td>
					{#if !project}
						<td class="col-scope">
							{#if r.project}<a class="muted" href="/projects/{r.project}">{r.project}</a
								>{:else}<span class="subtle">System</span>{/if}
						</td>
					{/if}
					<td>
						<span class="badges">
							<Badge>Pull</Badge>
							{#if r.canPush}<Badge tone="accent">Push</Badge>{/if}
							{#if r.canDelete}<Badge tone="warning">Delete</Badge>{/if}
						</span>
					</td>
					<td class="muted nowrap col-expires" title={r.expiresAt ? when(r.expiresAt) : ''}>
						{#if r.expiresAt}<Time unix={r.expiresAt} />{:else}never{/if}
					</td>
					<td class="muted nowrap col-used"><Time unix={r.lastUsedAt} /></td>
					<td class="shrink">
						<span class="row-actions">
							<Button size="sm" variant="ghost" onclick={() => editDraft(r)}>Edit</Button>
							<Button size="sm" variant="ghost" onclick={() => toggle(r)}
								>{r.disabled ? 'Enable' : 'Disable'}</Button
							>
							<ConfirmButton
								label="New secret"
								iconOnly
								icon="key"
								question="Replace the secret?"
								confirmLabel="Replace"
								onconfirm={() => rotate(r)}
							/>
							<ConfirmButton
								label={`Delete ${r.name}`}
								iconOnly
								icon="trash"
								question="Delete?"
								onconfirm={() => remove(r)}
							/>
						</span>
					</td>
				</tr>
			{/each}
		</tbody>
	</Table>
{/if}

<style>
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: var(--space-3);
		margin-bottom: var(--space-5);
	}

	.lede {
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.block {
		margin-bottom: var(--space-6);
	}

	.editor {
		display: grid;
		gap: var(--space-4);
		max-width: 40rem;
		padding: var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
	}

	.grid2 {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-4);
	}

	.grid2 :global(.select) {
		width: 100%;
	}

	.perms {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-5);
	}

	.perms__label {
		width: 100%;
		font-size: var(--text-sm);
		font-weight: 500;
	}

	.robot {
		display: grid;
		line-height: 1.35;
	}

	.robot__name {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		font-weight: 500;
	}

	.robot__desc {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	.badges {
		display: inline-flex;
		gap: var(--space-1);
	}

	tr.is-off td:not(:last-child) {
		opacity: 0.55;
	}

	@media (max-width: 56rem) {
		.col-used,
		.col-scope {
			display: none;
		}
	}

	@media (max-width: 40rem) {
		.grid2 {
			grid-template-columns: 1fr;
		}

		.col-expires {
			display: none;
		}

		.editor {
			padding: var(--space-4);
		}
	}
</style>
