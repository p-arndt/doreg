<script lang="ts">
	// A table-shaped placeholder; `columns` are relative widths of the shimmer bars.
	let { rows = 5, columns = [3, 1, 1, 1] }: { rows?: number; columns?: number[] } = $props();
</script>

<div class="skeleton" aria-busy="true" aria-label="Loading" role="status">
	{#each Array.from({ length: rows }, (_, i) => i) as r (r)}
		<div class="skeleton__row">
			{#each columns as c, i (i)}
				<span class="skeleton__cell" style:flex={c}>
					<span class="skeleton__bar" style:width="{i === 0 ? 45 + ((r * 23) % 40) : 60}%"></span>
				</span>
			{/each}
		</div>
	{/each}
</div>

<style>
	.skeleton {
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
		overflow: hidden;
	}

	.skeleton__row {
		display: flex;
		gap: var(--space-5);
		align-items: center;
		height: 2.75rem;
		padding: 0 var(--space-4);
		border-bottom: 1px solid var(--border);
	}

	.skeleton__row:last-child {
		border-bottom: 0;
	}

	.skeleton__cell {
		min-width: 0;
	}

	.skeleton__bar {
		display: block;
		height: 0.625rem;
		border-radius: var(--radius-pill);
		background: linear-gradient(
			90deg,
			var(--skeleton-a) 0%,
			var(--skeleton-b) 50%,
			var(--skeleton-a) 100%
		);
		background-size: 200% 100%;
		animation: shimmer 1.4s ease-in-out infinite;
	}

	@keyframes shimmer {
		from {
			background-position: 100% 0;
		}
		to {
			background-position: -100% 0;
		}
	}
</style>
