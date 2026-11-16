import { afterEach, describe, expect, it } from 'vitest';
import { guestRoute, repoCrumbs, session } from './session.svelte';

afterEach(() => {
	session.me = null;
	session.guest = false;
});

describe('guestRoute', () => {
	it('allows the repository list and repository pages', () => {
		expect(guestRoute('/')).toBe(true);
		expect(guestRoute('/r/team/app')).toBe(true);
		expect(guestRoute('/r/team/app:v1')).toBe(true);
	});

	it('keeps every other page behind the login', () => {
		for (const path of ['/projects', '/projects/team', '/robots', '/gc', '/account', '/rx']) {
			expect(guestRoute(path)).toBe(false);
		}
	});
});

describe('repoCrumbs', () => {
	it('links members to the project', () => {
		session.me = { user: { id: 1, username: 'u', role: 'user', createdAt: 0 }, version: 'dev' };
		expect(repoCrumbs('team').map((c) => c.href)).toEqual(['/projects', '/projects/team']);
	});

	it('links guests back to the public list', () => {
		session.guest = true;
		expect(repoCrumbs('team')).toEqual([{ href: '/', label: 'Repositories' }]);
	});
});
