import { describe, expect, it } from 'vitest';
import { defaultView, isGitHubUrl, sourceLabel } from './readme';

describe('isGitHubUrl', () => {
	it('accepts github.com over http and https', () => {
		expect(isGitHubUrl('https://github.com/owner/repo')).toBe(true);
		expect(isGitHubUrl('http://github.com/owner/repo')).toBe(true);
	});

	it('rejects other hosts, lookalike hosts and values without a web scheme', () => {
		expect(isGitHubUrl('https://gitlab.com/owner/repo')).toBe(false);
		expect(isGitHubUrl('https://github.com.example.org/owner/repo')).toBe(false);
		expect(isGitHubUrl('github.com/owner/repo')).toBe(false);
		expect(isGitHubUrl('')).toBe(false);
	});
});

describe('sourceLabel', () => {
	it('names github.com sources GitHub', () => {
		expect(sourceLabel('https://github.com/owner/repo')).toBe('GitHub');
	});

	it('shows the host for other web sources', () => {
		expect(sourceLabel('https://gitlab.example.com/team/app')).toBe('gitlab.example.com');
	});

	it('is empty for anything that is not a web URL', () => {
		expect(sourceLabel('javascript:alert(1)')).toBe('');
		expect(sourceLabel('')).toBe('');
	});
});

describe('defaultView', () => {
	it('opens the overview when the repository has a README', () => {
		expect(defaultView(true)).toBe('overview');
	});

	it('opens the tag list when it has none', () => {
		expect(defaultView(false)).toBe('tags');
	});
});
