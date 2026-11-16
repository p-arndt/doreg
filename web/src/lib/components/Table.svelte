<script lang="ts">
	import type { Snippet } from 'svelte';

	// Pages write plain <thead>/<tbody> markup; this component owns every table style.
	// Column helpers: th/td.right for numbers, .shrink for action columns, tr.is-selected for the current row.
	// A row with an `a.row-link` is clickable as a whole; the link stays the keyboard and middle-click target.
	// `.row-actions` are quick actions that show on hover or focus (always on touch screens).
	import { rowClick } from '#lib/rowclick.js';

	let { children, label, maxHeight }: { children: Snippet; label?: string; maxHeight?: string } =
		$props();
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="table-wrap" style:max-height={maxHeight} onclick={rowClick} onauxclick={rowClick}>
	<table aria-label={label}>
		{@render children()}
	</table>
</div>

<style>
	/* A rounded panel with a quiet header; rows parted by hairlines, numbers right-aligned in tabular figures. */
	.table-wrap {
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
		box-shadow: var(--shadow-control);
	}

	table {
		width: 100%;
		border-collapse: separate;
		border-spacing: 0;
		font-size: var(--text-base);
		font-variant-numeric: tabular-nums;
	}

	.table-wrap :global(thead th) {
		position: sticky;
		top: 0;
		z-index: 1;
		height: 2.25rem;
		padding: 0 var(--space-4);
		background: var(--surface);
		border-bottom: 1px solid var(--border);
		color: var(--fg-subtle);
		font-size: var(--text-xs);
		font-weight: 500;
		text-align: left;
		white-space: nowrap;
	}

	.table-wrap :global(tbody td) {
		height: 2.875rem;
		padding: var(--space-2) var(--space-4);
		border-bottom: 1px solid var(--border);
		vertical-align: middle;
	}

	.table-wrap :global(tbody tr:last-child td) {
		border-bottom: 0;
	}

	.table-wrap :global(tbody tr) {
		transition: background-color var(--dur-micro) var(--ease);
	}

	.table-wrap :global(tbody tr:hover) {
		background: var(--row-hover);
	}

	.table-wrap :global(td.right),
	.table-wrap :global(td .measure) {
		color: var(--fg-muted);
		font-variant-numeric: tabular-nums;
	}

	.table-wrap :global(tbody tr:has(a.row-link)) {
		cursor: pointer;
	}

	/* The chosen row takes a lime wash and a lime bar on its left edge; its text stays ink. */
	.table-wrap :global(tbody tr.is-selected),
	.table-wrap :global(tbody tr.is-selected:hover) {
		background: var(--row-selected);
	}

	.table-wrap :global(tbody tr.is-selected td:first-child) {
		box-shadow: inset 2px 0 0 var(--accent);
	}

	.table-wrap :global(.right) {
		text-align: right;
		font-variant-numeric: tabular-nums;
	}

	.table-wrap :global(.shrink) {
		width: 1%;
		white-space: nowrap;
	}

	@media (hover: hover) {
		.table-wrap :global(tbody tr .row-actions) {
			opacity: 0;
			transition: opacity var(--dur-micro) var(--ease);
		}

		.table-wrap :global(tbody tr:hover .row-actions),
		.table-wrap :global(tbody tr:focus-within .row-actions),
		.table-wrap :global(tbody tr .row-actions:has([role='group'])) {
			opacity: 1;
		}
	}

	/* The primary link of a row: plain text colour, the accent is for actions. */
	.table-wrap :global(a.row-link) {
		border-radius: var(--radius-xs);
		color: var(--fg);
		font-weight: 500;
		transition: color var(--dur-micro) var(--ease);
	}

	.table-wrap :global(a.row-link:hover) {
		color: var(--accent-fg);
		text-decoration: none;
	}

	@media (max-width: 40rem) {
		.table-wrap :global(thead th),
		.table-wrap :global(tbody td) {
			padding-inline: var(--space-3);
		}
	}
</style>
