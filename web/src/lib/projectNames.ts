import { api } from './api';
import type { Project } from './types';

/** Names of the projects the caller can see, for scope and filter selects. Empty on error. */
export async function projectNames(): Promise<string[]> {
	try {
		return (await api.get<Project[]>('/api/projects')).map((p) => p.name).sort();
	} catch {
		return [];
	}
}
