import type { Me, ProjectRole } from './types';

export const session = $state<{ me: Me | null }>({ me: null });

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
