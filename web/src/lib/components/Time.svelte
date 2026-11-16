<script lang="ts">
	import { ago, when } from '#lib/format.js';
	let { unix }: { unix: number } = $props();
</script>

<time datetime={unix ? new Date(unix * 1000).toISOString() : undefined} title={when(unix)}
	>{ago(unix)}</time
>
