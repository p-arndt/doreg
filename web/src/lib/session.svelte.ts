import type { Me, ProjectRole } from './types';

/** `guest` is a visitor without a session on a server with public browsing. */
export const session = $state<{ me: Me | null; guest: boolean }>({ me: null, guest: false });

/** Pages a guest may open: the repository list and repository or manifest pages. */
export function guestRoute(pathname: string) {
	return pathname === '/' || pathname.startsWith('/r/');
}

/** Breadcrumbs above a repository: its project for members, the public list for guests. */
export function repoCrumbs(project: string) {
	if (session.guest && !session.me) return [{ href: '/', label: 'Repositories' }];
	return [
		{ href: '/projects', label: 'Projects' },
		{ href: `/projects/${project}`, label: project }
	];
}

const projectRank: Record<ProjectRole, number> = { '': 0, read: 1, write: 2, admin: 3 };

/** Whether a project role reaches `min`; system admins always do. */
export function hasProjectRole(role: ProjectRole | undefined, min: Exclude<ProjectRole, ''>) {
	if (session.me?.user.role === 'admin') return true;
	return projectRank[role ?? ''] >= projectRank[min];
}

export const can = {
	admin: () => session.me?.user.role === 'admin',
	/** Manage members, robots, webhooks and settings of a project. */
	manage: (project: string) =>
		session.me?.user.role === 'admin' || !!session.me?.managedProjects?.includes(project),
	/** Manage at least one project: unlocks the audit log. */
	manageAny: () => session.me?.user.role === 'admin' || !!session.me?.managedProjects?.length
};
