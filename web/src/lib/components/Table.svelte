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
	/* Rim and app tables: horizontal rules only, no outer frame or card. */
	.table-wrap {
		overflow: auto;
		border-top: 1px solid var(--border-strong);
		border-bottom: 1px solid var(--border-strong);
	}

	table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--text-base);
	}

	.table-wrap :global(thead th) {
		position: sticky;
		top: 0;
		z-index: 1;
		height: 2.25rem;
		padding: 0 var(--space-3);
		background: var(--bg);
		border-bottom: 1px solid var(--border-strong);
		color: var(--fg-muted);
		font-size: var(--text-sm);
		font-weight: 500;
		text-align: left;
		white-space: nowrap;
	}

	.table-wrap :global(tbody td) {
		height: 2.75rem;
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--border);
		vertical-align: middle;
	}

	.table-wrap :global(tbody tr:last-child td) {
		border-bottom: 0;
	}

	.table-wrap :global(tbody tr) {
		transition: background-color 80ms var(--ease);
	}

	.table-wrap :global(tbody tr:hover) {
		background: var(--surface-hover);
	}

	.table-wrap :global(td.right),
	.table-wrap :global(td .measure) {
		font-family: var(--font-mono);
		font-size: var(--text-sm);
	}

	.table-wrap :global(tbody tr:has(a.row-link)) {
		cursor: pointer;
	}

	/* The row a click will open sinks to paper-3 and takes the lime square, Rim's mark for the chosen row. */
	.table-wrap :global(tbody td:first-child),
	.table-wrap :global(thead th:first-child) {
		position: relative;
		padding-left: var(--space-5);
	}

	.table-wrap :global(tbody tr:has(a.row-link):hover) {
		background: var(--bg-sidebar);
	}

	.table-wrap :global(tbody tr:has(a.row-link):hover td:first-child::before),
	.table-wrap :global(tbody tr.is-selected td:first-child::before) {
		content: '';
		position: absolute;
		top: calc(50% - 3px);
		left: var(--space-2);
		width: 6px;
		height: 6px;
		background: var(--accent);
	}

	.table-wrap :global(tbody tr.is-selected) {
		background: var(--surface-active);
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
			transition: opacity 80ms var(--ease);
		}

		.table-wrap :global(tbody tr:hover .row-actions),
		.table-wrap :global(tbody tr:focus-within .row-actions),
		.table-wrap :global(tbody tr .row-actions:has([role='group'])) {
			opacity: 1;
		}
	}

	/* The primary link of a row: plain text colour, the accent is for actions. */
	.table-wrap :global(a.row-link) {
		color: var(--fg);
		font-weight: 500;
	}

	.table-wrap :global(a.row-link:hover) {
		color: var(--accent-fg);
		text-decoration: none;
	}
</style>
