<script lang="ts" module>
	// Stroke icons on a 24px grid, drawn for doreg. Add new ones here instead of pulling in an icon library.
	const paths = {
		box: 'M21 8l-9-5-9 5 9 5 9-5zM3 8v8l9 5 9-5V8M12 13v8',
		folder:
			'M3 6.5A1.5 1.5 0 0 1 4.5 5H9l2 2h8.5A1.5 1.5 0 0 1 21 8.5v9a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 17.5z',
		bot: 'M5 9h14v10H5zM12 5v4M9 14h.01M15 14h.01M2 13v3M22 13v3',
		trash: 'M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13',
		recycle: 'M4 12a8 8 0 0 1 14-5.3M20 12a8 8 0 0 1-14 5.3M18 3v4h-4M6 21v-4h4',
		clock: 'M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z',
		list: 'M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01',
		webhook:
			'M9 17H5a3 3 0 1 1 2.6-4.5M12 7l3.5 6M15 17a3 3 0 1 0 0-.01M12 7a3 3 0 1 1 2.8 2M9 17l3-5',
		users:
			'M16 20v-1.5a3.5 3.5 0 0 0-3.5-3.5h-5A3.5 3.5 0 0 0 4 18.5V20M10 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7zM20 20v-1.5a3.5 3.5 0 0 0-2.5-3.35M15.5 4.15a3.5 3.5 0 0 1 0 6.7',
		settings:
			'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 13.5l1.6 1.2-2 3.4-1.9-.7a7 7 0 0 1-1.7 1l-.3 2H11l-.3-2a7 7 0 0 1-1.7-1l-1.9.7-2-3.4 1.6-1.2a7 7 0 0 1 0-3L5 9.3l2-3.4 1.9.7a7 7 0 0 1 1.7-1L11 3.6h4l.3 2a7 7 0 0 1 1.7 1l1.9-.7 2 3.4-1.6 1.2a7 7 0 0 1 0 3z',
		user: 'M19 20v-1.5a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4V20M12 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8z',
		logout: 'M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3M10 16l-4-4 4-4M6 12h10',
		copy: 'M9 9h10v11H9zM5 15V4h10',
		check: 'M5 12.5l4.5 4.5L19 7.5',
		x: 'M6 6l12 12M18 6L6 18',
		menu: 'M4 7h16M4 12h16M4 17h16',
		search: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4',
		chevronDown: 'M6 9l6 6 6-6',
		chevronRight: 'M9 6l6 6-6 6',
		chevronsUpDown: 'M8 9l4-4 4 4M8 15l4 4 4-4',
		plus: 'M12 5v14M5 12h14',
		alert:
			'M12 8v5M12 16.5h.01M10.3 3.9L2.6 17.5A2 2 0 0 0 4.3 20.5h15.4a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z',
		globe:
			'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18',
		layers: 'M12 3l9 5-9 5-9-5 9-5zM3 13l9 5 9-5',
		key: 'M14.5 9.5a4.5 4.5 0 1 1-9 0 4.5 4.5 0 0 1 9 0zM13.2 12.7L21 20.5M17 16.5l2-2M19 18.5l1.5-1.5',
		shield: 'M12 3l8 3v6c0 4.5-3.4 8.2-8 9-4.6-.8-8-4.5-8-9V6l8-3z',
		terminal: 'M4 17l6-5-6-5M12 19h8'
	} as const;
	export type IconName = keyof typeof paths;
</script>

<script lang="ts">
	let {
		name,
		size = 16,
		class: cls = ''
	}: { name: IconName; size?: number; class?: string } = $props();
</script>

<svg
	class="icon {cls}"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="1.75"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
	focusable="false"><path d={paths[name]} /></svg
>

<style>
	.icon {
		flex: none;
		display: block;
	}
</style>
