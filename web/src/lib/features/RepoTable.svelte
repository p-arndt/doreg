<script lang="ts">
	import { bytes, count } from '#lib/format.js';
	import Table from '#lib/components/Table.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import PullCopyButton from '#lib/components/PullCopyButton.svelte';
	import Time from '#lib/components/Time.svelte';
	import { session } from '#lib/session.svelte.js';
	import type { RepoListItem } from '#lib/types.js';

	let { repos, showProject = true }: { repos: RepoListItem[]; showProject?: boolean } = $props();
	const host = location.host;
</script>

<Table label="Repositories">
	<thead>
		<tr>
			<th>Name</th>
			{#if showProject}<th class="col-project">Project</th>{/if}
			<th class="right">Tags</th>
			<th class="right">Size</th>
			<th class="right col-pulls">Pulls</th>
			<th class="col-push">Last push</th>
			<th class="shrink"><span class="visually-hidden">Actions</span></th>
		</tr>
	</thead>
	<tbody>
		{#each repos as r (r.name)}
			<tr>
				<td>
					<span class="name">
						<a class="row-link" href="/r/{r.name}">{r.name}</a>
						{#if r.public}<Badge title="Anyone can pull without signing in">Public</Badge>{/if}
					</span>
					{#if r.description}<span class="desc" title={r.description}>{r.description}</span>{/if}
				</td>
				{#if showProject}
					<td class="col-project">
						{#if r.project && session.me}<a class="project" href="/projects/{r.project}"
								>{r.project}</a
							>{:else if r.project}<span class="project">{r.project}</span>{/if}
					</td>
				{/if}
				<td class="right muted">{r.tags}</td>
				<td class="right muted nowrap">{bytes(r.size)}</td>
				<td class="right muted nowrap col-pulls" title={r.pulls.toLocaleString()}
					>{count(r.pulls)}</td
				>
				<td class="muted nowrap col-push"><Time unix={r.lastPush} /></td>
				<td class="shrink">
					<span class="row-actions"><PullCopyButton reference={`${host}/${r.name}`} /></span>
				</td>
			</tr>
		{/each}
	</tbody>
</Table>

<style>
	.name {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	/* One line: long descriptions cut off instead of stretching the table. */
	.desc {
		display: block;
		max-width: 28rem;
		margin-top: 0.125rem;
		overflow: hidden;
		color: var(--fg-muted);
		font-size: var(--text-xs);
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.project {
		color: var(--fg-muted);
	}

	.project:hover {
		color: var(--fg);
	}

	@media (max-width: 48rem) {
		.col-project {
			display: none;
		}
	}

	@media (max-width: 40rem) {
		.col-push,
		.col-pulls {
			display: none;
		}
	}
</style>
