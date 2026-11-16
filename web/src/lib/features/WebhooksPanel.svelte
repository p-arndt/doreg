<script lang="ts">
	import { api, q } from '#lib/api.js';
	import { actionLabel } from '#lib/format.js';
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
	import Skeleton from '#lib/components/Skeleton.svelte';
	import Time from '#lib/components/Time.svelte';
	import type { Delivery, Webhook, WebhookList } from '#lib/types.js';

	// Webhooks of one project, or (project unset) all webhooks for administrators,
	// who can also create system webhooks that fire for every project.
	let { project, projects = [] }: { project?: string; projects?: string[] } = $props();

	interface Draft {
		id: number;
		project: string;
		name: string;
		url: string;
		secret: string;
		hasSecret: boolean;
		events: string[];
		enabled: boolean;
	}

	let list = $state<WebhookList | null>(null);
	let error = $state<unknown>(null);
	let draft = $state<Draft | null>(null);
	let saving = $state(false);
	let testing = $state(0);
	let openId = $state(0);
	let deliveries = $state<Delivery[] | null>(null);
	let deliveriesError = $state<unknown>(null);

	async function load() {
		error = null;
		try {
			list = await api.get<WebhookList>(`/api/webhooks${project ? `?project=${q(project)}` : ''}`);
		} catch (e) {
			error = e;
		}
	}
	load();

	function newDraft() {
		draft = {
			id: 0,
			project: project ?? '',
			name: '',
			url: '',
			secret: '',
			hasSecret: false,
			events: ['push', 'delete_tag'].filter((e) => list?.events.includes(e)),
			enabled: true
		};
	}

	function editDraft(w: Webhook) {
		draft = {
			id: w.id,
			project: w.project ?? '',
			name: w.name,
			url: w.url,
			secret: '',
			hasSecret: w.hasSecret,
			events: [...w.events],
			enabled: w.enabled
		};
	}

	function toggleEvent(ev: string, on: boolean) {
		if (!draft) return;
		draft.events = on ? [...draft.events, ev] : draft.events.filter((e) => e !== ev);
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!draft) return;
		saving = true;
		const body = {
			project: draft.project,
			name: draft.name,
			url: draft.url,
			secret: draft.secret,
			events: draft.events,
			enabled: draft.enabled
		};
		try {
			if (draft.id) await api.put(`/api/webhooks/${draft.id}`, body);
			else await api.post('/api/webhooks', body);
			notify('Webhook saved.');
			draft = null;
			await load();
		} catch (err) {
			fail(err);
		} finally {
			saving = false;
		}
	}

	async function test(w: Webhook) {
		testing = w.id;
		try {
			const d = await api.post<Delivery>(`/api/webhooks/${w.id}/test`);
			if (d.error || d.statusCode >= 300)
				notify(`Test failed: ${d.error || `HTTP ${d.statusCode}`}`, 'danger');
			else notify(`Test delivered: HTTP ${d.statusCode} in ${d.durationMs} ms.`);
			await load();
			if (openId === w.id) await loadDeliveries(w.id);
		} catch (e) {
			fail(e);
		} finally {
			testing = 0;
		}
	}

	async function loadDeliveries(id: number) {
		deliveriesError = null;
		try {
			deliveries = await api.get<Delivery[]>(`/api/webhooks/${id}/deliveries`);
		} catch (e) {
			deliveriesError = e;
		}
	}

	function toggleDeliveries(w: Webhook) {
		if (openId === w.id) {
			openId = 0;
			return;
		}
		openId = w.id;
		deliveries = null;
		loadDeliveries(w.id);
	}

	async function remove(w: Webhook) {
		try {
			await api.del(`/api/webhooks/${w.id}`);
			if (openId === w.id) openId = 0;
			await load();
		} catch (e) {
			fail(e);
		}
	}

	function pretty(payload: string) {
		try {
			return JSON.stringify(JSON.parse(payload), null, 2);
		} catch {
			return payload;
		}
	}

	const ok = (status: number, err: string) => !err && status >= 200 && status < 300;
</script>

<div class="head">
	<p class="lede">HTTP POST callbacks with a JSON payload when images are pushed or deleted.</p>
	{#if !draft && list}
		<Button variant="primary" icon="plus" onclick={newDraft}>New webhook</Button>
	{/if}
</div>

{#if draft && list}
	<form class="editor block" onsubmit={save}>
		<h2>{draft.id ? `Edit ${draft.name}` : 'New webhook'}</h2>
		<div class="grid2">
			{#if !project && !draft.id}
				<Field label="Scope" hint="System webhooks fire for every project.">
					<Select bind:value={draft.project}>
						<option value="">System (all projects)</option>
						{#each projects as p (p)}<option value={p}>{p}</option>{/each}
					</Select>
				</Field>
			{/if}
			<Field label="Name">
				<Input bind:value={draft.name} required placeholder="Deploy hook" />
			</Field>
		</div>
		<Field label="Payload URL">
			<Input type="url" bind:value={draft.url} required placeholder="https://ci.example.com/hook" />
		</Field>
		<Field
			label="Secret"
			hint="Signs each payload so the receiver can verify it came from doreg. Optional."
		>
			<Input
				type="password"
				bind:value={draft.secret}
				autocomplete="new-password"
				placeholder={draft.hasSecret ? 'Unchanged' : 'None'}
			/>
		</Field>
		<fieldset class="events">
			<legend>Events</legend>
			{#each list.events as ev (ev)}
				<Checkbox
					label={actionLabel(ev)}
					checked={draft.events.includes(ev)}
					onchange={(e) => toggleEvent(ev, e.currentTarget.checked)}
				/>
			{/each}
		</fieldset>
		<Checkbox label="Enabled" bind:checked={draft.enabled} />
		<div class="row">
			<Button variant="primary" type="submit" loading={saving} disabled={draft.events.length === 0}
				>Save webhook</Button
			>
			<Button variant="ghost" onclick={() => (draft = null)}>Cancel</Button>
		</div>
	</form>
{/if}

{#if error}
	<ErrorState {error} retry={load} />
{:else if !list}
	<Skeleton rows={3} columns={[3, 2, 2, 1]} />
{:else if list.webhooks.length === 0}
	{#if !draft}
		<EmptyState
			icon="webhook"
			title="No webhooks"
			description="Notify a deploy pipeline or chat when a new image arrives."
		/>
	{/if}
{:else}
	<Table label="Webhooks">
		<thead>
			<tr>
				<th>Webhook</th>
				{#if !project}<th class="col-scope">Scope</th>{/if}
				<th class="col-events">Events</th>
				<th>Last delivery</th>
				<th class="shrink"><span class="visually-hidden">Actions</span></th>
			</tr>
		</thead>
		<tbody>
			{#each list.webhooks as w (w.id)}
				<tr class:is-off={!w.enabled} class:is-selected={openId === w.id}>
					<td>
						<div class="hook">
							<span class="hook__name">
								{w.name}
								{#if !w.enabled}<Badge>Off</Badge>{/if}
							</span>
							<span class="hook__url" title={w.url}>{w.url}</span>
						</div>
					</td>
					{#if !project}
						<td class="col-scope">
							{#if w.project}<a class="muted" href="/projects/{w.project}">{w.project}</a
								>{:else}<span class="subtle">System</span>{/if}
						</td>
					{/if}
					<td class="col-events muted">
						{w.events.map(actionLabel).join(', ')}
					</td>
					<td class="nowrap">
						{#if w.lastAt}
							<span class="last">
								<Badge tone={ok(w.lastStatus, w.lastError) ? 'success' : 'danger'}
									>{w.lastStatus || 'error'}</Badge
								>
								<span class="muted" title={w.lastError}><Time unix={w.lastAt} /></span>
							</span>
						{:else}
							<span class="subtle">never</span>
						{/if}
					</td>
					<td class="shrink">
						<span class="row-actions">
							<Button size="sm" variant="ghost" loading={testing === w.id} onclick={() => test(w)}
								>Test</Button
							>
							<Button
								size="sm"
								variant="ghost"
								aria-expanded={openId === w.id}
								onclick={() => toggleDeliveries(w)}>Deliveries</Button
							>
							<Button size="sm" variant="ghost" onclick={() => editDraft(w)}>Edit</Button>
							<ConfirmButton
								label={`Delete ${w.name}`}
								iconOnly
								icon="trash"
								question="Delete?"
								onconfirm={() => remove(w)}
							/>
						</span>
					</td>
				</tr>
				{#if openId === w.id}
					<tr class="deliveries-row">
						<td colspan={project ? 4 : 5}>
							{#if deliveriesError}
								<ErrorState error={deliveriesError} retry={() => loadDeliveries(w.id)} />
							{:else if !deliveries}
								<Skeleton rows={2} columns={[2, 1, 1, 1]} />
							{:else if deliveries.length === 0}
								<p class="subtle">No deliveries yet. Use Test to send one.</p>
							{:else}
								<ul class="deliveries">
									{#each deliveries as d (d.id)}
										<li>
											<details>
												<summary>
													<Badge tone={ok(d.statusCode, d.error) ? 'success' : 'danger'}
														>{d.statusCode || 'error'}</Badge
													>
													<span class="d__event">{actionLabel(d.event)}</span>
													<span class="subtle num">{d.durationMs} ms</span>
													{#if d.attempt > 1}<span class="subtle">attempt {d.attempt}</span>{/if}
													<span class="d__time muted"><Time unix={d.at} /></span>
												</summary>
												{#if d.error}<p class="d__error">{d.error}</p>{/if}
												<pre>{pretty(d.payload)}</pre>
											</details>
										</li>
									{/each}
								</ul>
							{/if}
						</td>
					</tr>
				{/if}
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

	.events {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(10rem, 1fr));
		gap: var(--space-2) var(--space-4);
		margin: 0;
		padding: 0;
		border: 0;
	}

	legend {
		margin-bottom: var(--space-2);
		font-size: var(--text-sm);
		font-weight: 500;
	}

	.hook {
		display: grid;
		min-width: 0;
		line-height: 1.35;
	}

	.hook__name {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		font-weight: 500;
	}

	.hook__url {
		max-width: 22rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	.col-events {
		font-size: var(--text-sm);
	}

	.last {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
	}

	tr.is-off td:not(:last-child) {
		opacity: 0.55;
	}

	.deliveries-row td {
		background: var(--bg-sidebar);
	}

	.deliveries {
		display: grid;
		gap: 0.125rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	summary {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-1) 0;
		cursor: pointer;
		font-size: var(--text-sm);
	}

	.d__time {
		margin-left: auto;
	}

	.d__error {
		color: var(--danger);
		font-size: var(--text-sm);
	}

	pre {
		max-height: 16rem;
		overflow: auto;
		margin: var(--space-2) 0 var(--space-3);
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg);
		color: var(--fg-muted);
		font-size: var(--text-xs);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}

	@media (max-width: 56rem) {
		.col-events,
		.col-scope {
			display: none;
		}
	}

	@media (max-width: 40rem) {
		.grid2 {
			grid-template-columns: 1fr;
		}

		.editor {
			padding: var(--space-4);
		}
	}
</style>
