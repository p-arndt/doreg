import { api, q } from './api';
import { session } from './session.svelte';
import type { ProjectDetail, ProjectRole } from './types';

/** The caller's role in a project, "" when they cannot see it (e.g. a public repository only). */
export async function loadProjectRole(project: string): Promise<ProjectRole> {
	// Guests have no project roles; asking would answer 401 and send them to the login.
	if (!session.me) return '';
	try {
		const d = await api.get<ProjectDetail>(`/api/projects/${q(project)}`);
		return d.project.role;
	} catch {
		return '';
	}
}
