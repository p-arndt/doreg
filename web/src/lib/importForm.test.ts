import { describe, expect, it } from 'vitest';
import { importDone, parsePatterns } from './importForm';

describe('parsePatterns', () => {
	it('splits on commas, spaces and new lines and drops duplicates', () => {
		expect(parsePatterns(' team/*, app\nteam/*  tools/cli ')).toEqual([
			'team/*',
			'app',
			'tools/cli'
		]);
	});

	it('means all repositories when empty', () => {
		expect(parsePatterns('  \n ')).toEqual([]);
	});
});

describe('importDone', () => {
	it('counts copied, skipped and failed tags', () => {
		expect(importDone({ tags: 4, tagsCopied: 1, tagsSkipped: 1, tagsFailed: 1 })).toBe(0.75);
	});

	it('is 0 before tags are known', () => {
		expect(importDone({ tags: 0, tagsCopied: 0, tagsSkipped: 0, tagsFailed: 0 })).toBe(0);
	});
});
