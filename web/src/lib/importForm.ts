/** Repository patterns typed as a list separated by commas, spaces or new lines. */
export function parsePatterns(input: string): string[] {
	return [...new Set(input.split(/[\s,]+/).filter(Boolean))];
}

/** Tags handled so far as a 0–1 fraction. */
export function importDone(p: {
	tags: number;
	tagsCopied: number;
	tagsSkipped: number;
	tagsFailed: number;
}): number {
	if (!p.tags) return 0;
	return Math.min(1, (p.tagsCopied + p.tagsSkipped + p.tagsFailed) / p.tags);
}
