/** Parses an absolute http(s) URL. Other schemes never become link targets, so a javascript: URL cannot reach an href. */
export function httpUrl(value: string): URL | null {
	try {
		const url = new URL(value.trim());
		return url.protocol === 'https:' || url.protocol === 'http:' ? url : null;
	} catch {
		return null;
	}
}

/** Whether the URL points at github.com, the host the README import can fetch from. */
export function isGitHubUrl(value: string): boolean {
	return httpUrl(value)?.hostname === 'github.com';
}

/** Link text for a source URL: "GitHub" for github.com, otherwise the host; "" when it is not a web URL. */
export function sourceLabel(value: string): string {
	const url = httpUrl(value);
	if (!url) return '';
	return url.hostname === 'github.com' ? 'GitHub' : url.host;
}

/** A repository with a README opens on the overview; without one the tag list stays the landing view. */
export function defaultView(hasReadme: boolean): 'overview' | 'tags' {
	return hasReadme ? 'overview' : 'tags';
}
