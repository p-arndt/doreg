<script lang="ts">
	// The HTML is rendered and sanitized by the server (readmeHtml, preview), never by the browser.
	let { html }: { html: string } = $props();
</script>

<!-- eslint-disable-next-line svelte/no-at-html-tags -- the server sanitizes this HTML before it is sent -->
<div class="markdown">{@html html}</div>

<style>
	.markdown {
		min-width: 0;
		max-width: 48rem;
		color: var(--fg);
		font-size: var(--text-base);
		line-height: 1.65;
		overflow-wrap: break-word;
	}

	.markdown :global(p),
	.markdown :global(ul),
	.markdown :global(ol),
	.markdown :global(pre),
	.markdown :global(table),
	.markdown :global(blockquote),
	.markdown :global(hr) {
		margin: 0 0 var(--space-3);
	}

	.markdown :global(h1),
	.markdown :global(h2),
	.markdown :global(h3),
	.markdown :global(h4),
	.markdown :global(h5),
	.markdown :global(h6) {
		margin: var(--space-5) 0 var(--space-3);
		font-weight: 600;
		letter-spacing: -0.015em;
		line-height: 1.25;
	}

	.markdown :global(h1) {
		font-size: var(--text-xl);
		padding-bottom: 0.3em;
		border-bottom: 1px solid var(--border);
	}

	.markdown :global(h2) {
		font-size: var(--text-lg);
		padding-bottom: 0.3em;
		border-bottom: 1px solid var(--border);
	}

	.markdown :global(h3) {
		font-size: var(--text-md);
	}

	.markdown :global(h4),
	.markdown :global(h5),
	.markdown :global(h6) {
		font-size: var(--text-base);
		color: var(--fg-muted);
	}

	.markdown :global(:first-child) {
		margin-top: 0;
	}

	.markdown :global(ul),
	.markdown :global(ol) {
		padding-left: 1.5rem;
	}

	.markdown :global(li + li) {
		margin-top: 0.25rem;
	}

	/* Task lists drop the bullets; the checkbox carries the marker. */
	.markdown :global(ul:has(> li > input[type='checkbox'])) {
		padding-left: 0;
		list-style: none;
	}

	.markdown :global(input[type='checkbox']) {
		margin: 0 0.4em 0 0;
		vertical-align: middle;
		accent-color: var(--accent);
	}

	.markdown :global(a) {
		color: var(--fg);
		text-decoration: underline;
		text-decoration-color: var(--border-strong);
		text-underline-offset: 3px;
	}

	.markdown :global(a:hover) {
		text-decoration-color: var(--fg);
	}

	.markdown :global(code) {
		padding: 0.1em 0.35em;
		border-radius: var(--radius-xs);
		background: var(--surface-raised);
		font-family: var(--font-mono);
		font-size: 0.85em;
	}

	.markdown :global(pre) {
		overflow-x: auto;
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		line-height: 1.5;
	}

	.markdown :global(pre code) {
		padding: 0;
		background: none;
		font-size: inherit;
	}

	.markdown :global(table) {
		display: block;
		max-width: 100%;
		overflow-x: auto;
		border-collapse: collapse;
	}

	.markdown :global(th),
	.markdown :global(td) {
		padding: 0.375rem 0.75rem;
		border: 1px solid var(--border);
		text-align: left;
		vertical-align: top;
	}

	.markdown :global(th) {
		background: var(--surface);
		font-weight: 600;
	}

	.markdown :global(blockquote) {
		padding: 0 var(--space-4);
		border-left: 3px solid var(--border-strong);
		color: var(--fg-muted);
	}

	/* Inline, as on GitHub: badges sit side by side and align="center" centers them. The base reset makes images blocks. */
	.markdown :global(img) {
		display: inline-block;
		max-width: 100%;
		height: auto;
		vertical-align: middle;
	}

	.markdown :global(a:has(> img)) {
		text-decoration: none;
	}

	.markdown :global([align='center']) {
		text-align: center;
	}

	.markdown :global([align='right']) {
		text-align: right;
	}

	.markdown :global(img[align='right']) {
		float: right;
		margin: 0 0 var(--space-3) var(--space-3);
	}

	.markdown :global(img[align='left']) {
		float: left;
		margin: 0 var(--space-3) var(--space-3) 0;
	}

	.markdown :global(details) {
		margin: 0 0 var(--space-3);
	}

	.markdown :global(summary) {
		cursor: pointer;
		font-weight: 500;
	}

	.markdown :global(hr) {
		height: 0;
		border: 0;
		border-top: 1px solid var(--border);
		margin: var(--space-5) 0;
	}
</style>
