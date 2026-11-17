import { describe, expect, it } from 'vitest';
import {
	ago,
	bytes,
	count,
	describePolicy,
	duration,
	fraction,
	healthcheckCommand,
	healthcheckTiming,
	historyCommand,
	mediaTypeLabel,
	normalizeUserCode,
	parseRef,
	projectOf,
	pullText,
	splitReference,
	shellQuote,
	actionLabel,
	dateToUnix,
	unixToDate,
	gbToBytes,
	plural,
	refPath,
	safeNext,
	shortDigest
} from './format';

describe('bytes', () => {
	it('uses decimal units like docker', () => {
		expect(bytes(0)).toBe('0 B');
		expect(bytes(999)).toBe('999 B');
		expect(bytes(3_450_000)).toBe('3.45 MB');
		expect(bytes(123_400_000)).toBe('123 MB');
		expect(bytes(-1)).toBe('unknown');
	});
});

describe('count', () => {
	it('abbreviates large counts', () => {
		expect(count(0)).toBe('0');
		expect(count(999)).toBe('999');
		expect(count(1_234)).toBe('1.2K');
		expect(count(3_400_000)).toBe('3.4M');
	});
});

describe('refs', () => {
	const d = 'sha256:' + 'a'.repeat(64);
	it('parses tags and digests', () => {
		expect(parseRef('team/app:v1')).toEqual({ repo: 'team/app', ref: 'v1' });
		expect(parseRef(`team/app@${d}`)).toEqual({ repo: 'team/app', ref: d });
		expect(parseRef('team/app')).toEqual({ repo: 'team/app', ref: '' });
	});
	it('round-trips through refPath', () => {
		expect(refPath('team/app', 'v1')).toBe('/r/team/app:v1');
		expect(parseRef(refPath('team/app', d).slice(3))).toEqual({ repo: 'team/app', ref: d });
	});
	it('shortens digests', () => {
		expect(shortDigest(d)).toBe('aaaaaaaaaaaa');
	});
});

describe('ago', () => {
	it('formats relative times', () => {
		expect(ago(1000 - 7200, 1000)).toBe('2 hours ago');
		expect(ago(0)).toBe('never');
	});
});

describe('safeNext', () => {
	it('only allows same-origin paths', () => {
		expect(safeNext('/r/app')).toBe('/r/app');
		expect(safeNext('//evil.example')).toBe('/');
		expect(safeNext('/\\evil.example')).toBe('/');
		expect(safeNext('https://evil.example')).toBe('/');
		expect(safeNext(null)).toBe('/');
	});
});

describe('fraction', () => {
	it('clamps and survives a zero total', () => {
		expect(fraction(1, 4)).toBe(0.25);
		expect(fraction(5, 4)).toBe(1);
		expect(fraction(1, 0)).toBe(0);
	});
});

describe('plural', () => {
	it('picks the noun form', () => {
		expect(plural(1, 'tag')).toBe('1 tag');
		expect(plural(2, 'tag')).toBe('2 tags');
		expect(plural(1200, 'repository', 'repositories')).toBe('1,200 repositories');
	});
});

describe('mediaTypeLabel', () => {
	it('names known types and shortens the rest', () => {
		expect(mediaTypeLabel('application/vnd.oci.image.index.v1+json')).toBe('OCI image index');
		expect(mediaTypeLabel('application/vnd.example.thing+json')).toBe('example.thing');
	});
});

describe('historyCommand', () => {
	it('strips the shell wrapper', () => {
		expect(historyCommand('/bin/sh -c #(nop)  CMD ["app"]')).toBe('CMD ["app"]');
		expect(historyCommand('/bin/sh -c apk add curl')).toBe('RUN apk add curl');
		expect(historyCommand(undefined)).toBe('');
	});
});

describe('describePolicy', () => {
	it('summarises the rule', () => {
		expect(describePolicy({ keepLast: 10, olderThanDays: 0 })).toBe(
			'keep the newest 10, delete the rest'
		);
		expect(describePolicy({ keepLast: 5, olderThanDays: 30 })).toBe(
			'keep the newest 5, delete the rest older than 30 days'
		);
		expect(describePolicy({ keepLast: 0, olderThanDays: 7 })).toBe('delete tags older than 7 days');
	});
});

describe('pullText', () => {
	const ref = 'reg.example.com/team/api:1.0';
	it('copies the bare reference by default mode', () => {
		expect(pullText(ref, 'ref')).toBe(ref);
		expect(pullText(ref, 'ref', 'linux/arm64')).toBe(ref);
	});
	it('builds the pull command with an optional platform', () => {
		expect(pullText(ref, 'command')).toBe(`docker pull ${ref}`);
		expect(pullText(ref, 'command', 'linux/arm64')).toBe(
			`docker pull --platform linux/arm64 ${ref}`
		);
	});
});

describe('splitReference', () => {
	it('splits before the last path segment', () => {
		expect(splitReference('reg.example.com/team/api:1.0')).toEqual({
			head: 'reg.example.com/team/',
			tail: 'api:1.0'
		});
		expect(splitReference('reg.example.com/team/api@sha256:abc')).toEqual({
			head: 'reg.example.com/team/',
			tail: 'api@sha256:abc'
		});
	});
	it('keeps a reference without a slash whole', () => {
		expect(splitReference('api:1.0')).toEqual({ head: '', tail: 'api:1.0' });
	});
});

describe('projectOf', () => {
	it('uses the first segment or library', () => {
		expect(projectOf('team/api')).toBe('team');
		expect(projectOf('team/sub/api')).toBe('team');
		expect(projectOf('alpine')).toBe('library');
	});
});

describe('shellQuote', () => {
	it('quotes robot names with $', () => {
		expect(shellQuote('alice')).toBe('alice');
		expect(shellQuote('robot$team+ci')).toBe(`'robot$team+ci'`);
		expect(shellQuote("it's")).toBe(`'it'\\''s'`);
	});
});

describe('actionLabel', () => {
	it('humanises snake case', () => {
		expect(actionLabel('delete_tag')).toBe('Delete tag');
		expect(actionLabel('push')).toBe('Push');
	});
});

describe('dates', () => {
	it('round-trips a calendar day', () => {
		expect(unixToDate(dateToUnix('2026-03-04'))).toBe('2026-03-04');
		expect(dateToUnix('')).toBe(0);
		expect(unixToDate(0)).toBe('');
	});
});

describe('gbToBytes', () => {
	it('converts decimal gigabytes', () => {
		expect(gbToBytes(1.5)).toBe(1_500_000_000);
		expect(gbToBytes('')).toBe(0);
		expect(gbToBytes(-1)).toBe(0);
	});
});

describe('normalizeUserCode', () => {
	it('uppercases and restores the dash', () => {
		expect(normalizeUserCode('bcdfghjk')).toBe('BCDF-GHJK');
		expect(normalizeUserCode(' bcdf ghjk ')).toBe('BCDF-GHJK');
		expect(normalizeUserCode('BCDF-GHJK')).toBe('BCDF-GHJK');
		expect(normalizeUserCode('bcd')).toBe('BCD');
	});
});

describe('healthcheckCommand', () => {
	it('renders each HEALTHCHECK form like the Dockerfile', () => {
		expect(healthcheckCommand(['CMD-SHELL', 'curl -f http://localhost/ || exit 1'])).toBe(
			'curl -f http://localhost/ || exit 1'
		);
		expect(healthcheckCommand(['CMD', '/healthz', '--quiet'])).toBe('["/healthz","--quiet"]');
		expect(healthcheckCommand(['NONE'])).toBe('disabled');
		expect(healthcheckCommand([])).toBe('');
		expect(healthcheckCommand(undefined)).toBe('');
	});
});

describe('duration', () => {
	it('formats nanoseconds like Go', () => {
		expect(duration(30e9)).toBe('30s');
		expect(duration(90e9)).toBe('1m30s');
		expect(duration(3600e9)).toBe('1h');
		expect(duration(500e6)).toBe('500ms');
		expect(duration(0)).toBe('0s');
	});
});

describe('healthcheckTiming', () => {
	it('lists only the options that were set', () => {
		expect(healthcheckTiming({ Interval: 30e9, Timeout: 3e9, Retries: 3 })).toBe(
			'every 30s, timeout 3s, 3 retries'
		);
		expect(healthcheckTiming({ StartPeriod: 5e9, StartInterval: 1e9, Retries: 1 })).toBe(
			'start period 5s, start interval 1s, 1 retry'
		);
		expect(healthcheckTiming({})).toBe('');
	});
});
