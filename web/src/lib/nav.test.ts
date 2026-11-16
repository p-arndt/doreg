import { describe, expect, it } from 'vitest';
import { groupNav, isActive, navItems, visibleNav } from './nav';
import type { Me } from './types';

const me = (role: 'admin' | 'user', managedProjects: string[] = []): Me => ({
	user: { id: 1, username: 'u', role, createdAt: 0 },
	version: 'dev',
	managedProjects
});

describe('visibleNav', () => {
	it('shows plain users only the shared pages', () => {
		expect(visibleNav(me('user')).map((i) => i.href)).toEqual(['/', '/projects']);
		expect(visibleNav(null)).toEqual([]);
	});

	it('adds the audit log for project managers', () => {
		expect(visibleNav(me('user', ['team'])).map((i) => i.href)).toEqual([
			'/',
			'/projects',
			'/audit'
		]);
	});

	it('shows admins every finished page', () => {
		expect(visibleNav(me('admin')).map((i) => i.href)).toEqual([
			'/',
			'/projects',
			'/gc',
			'/retention',
			'/audit',
			'/webhooks',
			'/robots',
			'/users'
		]);
	});

	it('shows a page once it is marked ready', () => {
		const items = navItems.map((i) => (i.href === '/settings' ? { ...i, ready: true } : i));
		expect(visibleNav(me('admin'), items).map((i) => i.href)).toContain('/settings');
	});
});

describe('groupNav', () => {
	it('puts a plain user into the Registry section only', () => {
		const groups = groupNav(visibleNav(me('user')));
		expect(groups.map((g) => g.group)).toEqual(['Registry']);
		expect(groups[0].items.map((i) => i.href)).toEqual(['/', '/projects']);
	});

	it('orders admin sections Registry, Operations, Access', () => {
		const groups = groupNav(visibleNav(me('admin')));
		expect(groups.map((g) => g.group)).toEqual(['Registry', 'Operations', 'Access']);
		expect(groups[1].items.map((i) => i.href)).toEqual([
			'/gc',
			'/retention',
			'/audit',
			'/webhooks'
		]);
	});

	it('drops empty sections', () => {
		expect(groupNav([])).toEqual([]);
	});
});

describe('isActive', () => {
	it('treats repository pages as part of Repositories', () => {
		expect(isActive({ href: '/' }, '/r/team/api:v1')).toBe(true);
		expect(isActive({ href: '/' }, '/gc')).toBe(false);
		expect(isActive({ href: '/projects' }, '/projects/team')).toBe(true);
		expect(isActive({ href: '/users' }, '/usersx')).toBe(false);
	});
});
