import type { IconName } from './components/Icon.svelte';
import type { Me } from './types';

/** Who sees a nav item: everyone, project managers (and admins), or system admins only. */
export type Access = 'all' | 'manager' | 'admin';

/** Sidebar section a nav item belongs to; sections appear in this order. */
export type NavGroup = 'Registry' | 'Operations' | 'Access' | 'System';

const groupOrder: NavGroup[] = ['Registry', 'Operations', 'Access', 'System'];

export interface NavItem {
	href: string;
	label: string;
	icon: IconName;
	access: Access;
	group: NavGroup;
	/** Off until the page exists; flip to true when the route lands. */
	ready: boolean;
}

/** Sidebar destinations in display order, grouped by section. */
export const navItems: NavItem[] = [
	{ href: '/', label: 'Repositories', icon: 'box', access: 'all', group: 'Registry', ready: true },
	{
		href: '/projects',
		label: 'Projects',
		icon: 'folder',
		access: 'all',
		group: 'Registry',
		ready: true
	},
	{
		href: '/gc',
		label: 'Garbage collection',
		icon: 'recycle',
		access: 'admin',
		group: 'Operations',
		ready: true
	},
	{
		href: '/retention',
		label: 'Retention',
		icon: 'clock',
		access: 'admin',
		group: 'Operations',
		ready: true
	},
	{
		href: '/audit',
		label: 'Audit log',
		icon: 'list',
		access: 'manager',
		group: 'Operations',
		ready: true
	},
	{
		href: '/webhooks',
		label: 'Webhooks',
		icon: 'webhook',
		access: 'admin',
		group: 'Operations',
		ready: true
	},
	{
		href: '/robots',
		label: 'Robot accounts',
		icon: 'bot',
		access: 'admin',
		group: 'Access',
		ready: true
	},
	{ href: '/users', label: 'Users', icon: 'users', access: 'admin', group: 'Access', ready: true },
	{
		href: '/settings',
		label: 'Settings',
		icon: 'settings',
		access: 'admin',
		group: 'System',
		ready: false
	}
];

export function visibleNav(me: Me | null | undefined, items = navItems): NavItem[] {
	if (!me) return [];
	const admin = me.user.role === 'admin';
	const manager = admin || !!me.managedProjects?.length;
	return items.filter(
		(i) =>
			i.ready &&
			(i.access === 'all' || (i.access === 'manager' && manager) || (i.access === 'admin' && admin))
	);
}

/** Splits items into their sections, keeping item order and dropping empty sections. */
export function groupNav(items: NavItem[]): { group: NavGroup; items: NavItem[] }[] {
	return groupOrder
		.map((group) => ({ group, items: items.filter((i) => i.group === group) }))
		.filter((section) => section.items.length > 0);
}

/** Whether a nav item is the current section; repository detail pages belong to "/". */
export function isActive(item: Pick<NavItem, 'href'>, pathname: string): boolean {
	if (item.href === '/') return pathname === '/' || pathname.startsWith('/r/');
	return pathname === item.href || pathname.startsWith(item.href + '/');
}
