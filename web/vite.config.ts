import { defineConfig } from 'vitest/config';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';

const backend = 'http://localhost:8080';

interface ProxyReq {
	getHeader(name: string): unknown;
	setHeader(name: string, value: string): void;
}

export default defineConfig({
	server: {
		// Fixed, because the dev OIDC client allows exactly this redirect origin.
		port: 5173,
		strictPort: true,
		// doreg rejects cross-origin writes, so the proxy presents itself as the backend's own origin.
		proxy: Object.fromEntries(
			['/api', '/v2'].map((path) => [
				path,
				{
					target: backend,
					changeOrigin: true,
					configure: (proxy: { on: (e: 'proxyReq', fn: (req: ProxyReq) => void) => void }) =>
						proxy.on('proxyReq', (req) => {
							if (req.getHeader('origin')) req.setHeader('origin', backend);
						})
				}
			])
		)
	},
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({ pages: '../internal/webui/dist', fallback: 'index.html' })
		})
	],
	test: {
		expect: { requireAssertions: true },
		projects: [
			{
				extends: './vite.config.ts',
				test: {
					name: 'server',
					environment: 'node',
					include: ['src/**/*.{test,spec}.{js,ts}'],
					exclude: ['src/**/*.svelte.{test,spec}.{js,ts}']
				}
			}
		]
	}
});
