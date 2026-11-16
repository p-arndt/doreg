import { api, q } from './api';
import type { ProjectDetail, ProjectRole } from './types';

/** The caller's role in a project, "" when they cannot see it (e.g. a public repository only). */
export async function loadProjectRole(project: string): Promise<ProjectRole> {
	try {
		const d = await api.get<ProjectDetail>(`/api/projects/${q(project)}`);
		return d.project.role;
	} catch {
		return '';
	}
}
