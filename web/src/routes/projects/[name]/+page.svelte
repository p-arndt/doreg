<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, q } from '#lib/api.js';
	import { gbToBytes, plural } from '#lib/format.js';
	import { can, hasProjectRole } from '#lib/session.svelte.js';
	import { notify, fail } from '#lib/toast.svelte.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import Section from '#lib/components/Section.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Button from '#lib/components/Button.svelte';
	import Checkbox from '#lib/components/Checkbox.svelte';
	import Field from '#lib/components/Field.svelte';
	import Input from '#lib/components/Input.svelte';
	import PullReference from '#lib/components/PullReference.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import UsageBar from '#lib/components/UsageBar.svelte';
	import Time from '#lib/components/Time.svelte';
	import Facts from '#lib/components/Facts.svelte';
	import Fact from '#lib/components/Fact.svelte';
	import RepoTable from '#lib/features/RepoTable.svelte';
	import MembersPanel from '#lib/features/MembersPanel.svelte';
	import RobotsPanel from '#lib/features/RobotsPanel.svelte';
	import WebhooksPanel from '#lib/features/WebhooksPanel.svelte';
	import AuditLog from '#lib/features/AuditLog.svelte';
	import type { ProjectDetail, RepoListItem } from '#lib/types.js';

	const name = $derived(page.params.name ?? '');
	let detail = $state<ProjectDetail | null>(null);
	let repos = $state<RepoListItem[] | null>(null);
	let error = $state<unknown>(null);
	let reposError = $state<unknown>(null);
	let quotaGb = $state('');
	let savingQuota = $state(false);
	let confirmName = $state('');
	let deleting = $state(false);

	const host = location.host;

	async function load() {
		error = null;
		try {
			detail = await api.get<ProjectDetail>(`/api/projects/${q(name)}`);
			quotaGb = detail.project.quotaBytes ? String(detail.project.quotaBytes / 1e9) : '';
		} catch (e) {
			error = e;
		}
	}

	async function loadRepos() {
		reposError = null;
		try {
			repos = await api.get<RepoListItem[]>(`/api/repos?project=${q(name)}`);
		} catch (e) {
			reposError = e;
		}
	}

	$effect(() => {
		void name;
		detail = null;
		repos = null;
		load();
		loadRepos();
	});

	const p = $derived(detail?.project);
	const manage = $derived(!!p && (hasProjectRole(p.role, 'admin') || can.manage(name)));

	const tabs = $derived([
		{ key: 'repositories', label: 'Repositories', href: '?tab=repositories', count: repos?.length },
		...(manage
			? [
					{
						key: 'members',
						label: 'Members',
						href: '?tab=members',
						count: detail?.members?.length
					},
					{ key: 'robots', label: 'Robot accounts', href: '?tab=robots' },
					{ key: 'webhooks', label: 'Webhooks', href: '?tab=webhooks' },
					{ key: 'audit', label: 'Audit log', href: '?tab=audit' },
					{ key: 'settings', label: 'Settings', href: '?tab=settings' }
				]
			: [])
	]);
	const tab = $derived.by(() => {
		const want = page.url.searchParams.get('tab');
		return tabs.find((t) => t.key === want)?.key ?? 'repositories';
	});

	const roleLabel: Record<string, string> = {
		read: 'Read access',
		write: 'Write access',
		admin: 'Project admin'
	};

	async function setPublic(pub: boolean) {
		try {
			await api.patch(`/api/projects/${q(name)}`, { public: pub });
			notify(pub ? 'Anyone can now pull from this project.' : 'Pulling now requires access.');
			await load();
		} catch (e) {
			fail(e);
		}
	}

	async function saveQuota(e: SubmitEvent) {
		e.preventDefault();
		savingQuota = true;
		try {
			await api.patch(`/api/projects/${q(name)}`, { quotaBytes: gbToBytes(quotaGb) });
			notify('Quota saved.');
			await load();
		} catch (err) {
			fail(err);
		} finally {
			savingQuota = false;
		}
	}

	async function remove() {
		deleting = true;
		try {
			await api.del(`/api/projects/${q(name)}`);
			notify(`Deleted project ${name}.`);
			await goto('/projects');
		} catch (e) {
			fail(e);
		} finally {
			deleting = false;
		}
	}
</script>

<svelte:head><title>{name} · Projects · doreg</title></svelte:head>

<PageHeader title={name} crumbs={[{ href: '/projects', label: 'Projects' }]}>
	{#snippet meta()}
		{#if p}
			{#if p.public}<Badge tone="accent">Public</Badge>{:else}<Badge>Private</Badge>{/if}
		{/if}
	{/snippet}
</PageHeader>

{#if p}
	<div class="facts-row">
		<Facts label="Project">
			<Fact label="Repositories" measure>{p.repositories}</Fact>
			<Fact label={p.quotaBytes ? 'Storage of quota' : 'Storage'}>
				<UsageBar used={p.usedBytes} quota={p.quotaBytes} />
			</Fact>
			<Fact label="Your access">{roleLabel[p.role] ?? 'Public access'}</Fact>
			<Fact label="Created"><Time unix={p.createdAt} /></Fact>
		</Facts>
	</div>
{/if}

{#if error}
	<ErrorState {error} retry={load} />
{:else if !detail}
	<Skeleton rows={4} columns={[3, 1, 1, 2]} />
{:else}
	<Tabs label="Project sections" items={tabs} active={tab} />

	{#if tab === 'repositories'}
		{#if reposError}
			<ErrorState error={reposError} retry={loadRepos} />
		{:else if !repos}
			<Skeleton rows={4} columns={[4, 1, 1, 2]} />
		{:else if repos.length === 0}
			<EmptyState
				icon="box"
				title="No repositories yet"
				description="Push an image whose name starts with this project."
			>
				<PullReference reference={`${host}/${name}/my-app:latest`} />
			</EmptyState>
		{:else}
			<RepoTable {repos} showProject={false} />
		{/if}
	{:else if tab === 'members' && detail.members}
		<MembersPanel project={name} members={detail.members} onchange={load} />
	{:else if tab === 'robots'}
		<RobotsPanel project={name} />
	{:else if tab === 'webhooks'}
		<WebhooksPanel project={name} />
	{:else if tab === 'audit'}
		<AuditLog project={name} />
	{:else if tab === 'settings' && p}
		<div class="settings">
			<Section title="Visibility">
				<div class="card">
					<Checkbox
						label="Public project"
						description="Anyone can pull every repository in this project without an account."
						checked={p.public}
						onchange={(e) => setPublic(e.currentTarget.checked)}
					/>
				</div>
			</Section>

			<Section
				title="Quota"
				description={can.admin()
					? 'Pushes that would exceed the quota are rejected.'
					: 'Set by administrators. Pushes that would exceed it are rejected.'}
			>
				<form class="card quota" onsubmit={saveQuota}>
					<Field label="Quota (GB)" hint="Empty or 0 means no limit.">
						<Input
							type="number"
							min={0}
							step="any"
							bind:value={quotaGb}
							placeholder="No limit"
							disabled={!can.admin()}
						/>
					</Field>
					{#if can.admin()}
						<Button type="submit" loading={savingQuota}>Save quota</Button>
					{/if}
				</form>
			</Section>

			{#if can.admin()}
				<Section title="Danger zone">
					<div class="card card--danger">
						<div class="danger__text">
							<h3>Delete this project</h3>
							<p class="muted">
								Removes the project with its members, robot accounts and webhooks.
								{#if p.repositories}Delete its {plural(
										p.repositories,
										'repository',
										'repositories'
									)}
									first.{/if}
							</p>
						</div>
						<div class="danger__form">
							<Input
								placeholder={name}
								aria-label="Type the project name to confirm"
								bind:value={confirmName}
								mono
							/>
							<Button
								variant="danger-solid"
								disabled={confirmName !== name}
								loading={deleting}
								onclick={remove}>Delete project</Button
							>
						</div>
					</div>
				</Section>
			{/if}
		</div>
	{/if}
{/if}

<style>
	.facts-row {
		margin: calc(-1 * var(--space-2)) 0 var(--space-5);
	}

	.settings {
		max-width: 44rem;
	}

	.card {
		padding: var(--space-4);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
	}

	.quota {
		display: flex;
		align-items: flex-end;
		flex-wrap: wrap;
		gap: var(--space-3);
	}

	.quota :global(.field) {
		flex: 0 1 14rem;
	}

	.card--danger {
		display: grid;
		gap: var(--space-3);
		border-color: var(--danger-line);
	}

	.danger__text {
		display: grid;
		gap: var(--space-1);
		font-size: var(--text-sm);
	}

	.danger__form {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.danger__form :global(.input) {
		flex: 1 1 14rem;
		max-width: 22rem;
	}
</style>
