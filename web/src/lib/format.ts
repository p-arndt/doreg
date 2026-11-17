const units = ['B', 'kB', 'MB', 'GB', 'TB'];

/** Decimal units, matching what `docker images` and Docker Hub show. */
export function bytes(n: number): string {
	if (!Number.isFinite(n) || n < 0) return 'unknown';
	let i = 0;
	let v = n;
	while (v >= 1000 && i < units.length - 1) {
		v /= 1000;
		i++;
	}
	const digits = i === 0 || v >= 100 ? 0 : v >= 10 ? 1 : 2;
	return `${v.toFixed(digits)} ${units[i]}`;
}

const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 });

/** Short count for tables, like Docker Hub: 999, 1.2K, 3.4M. */
export function count(n: number): string {
	return compact.format(n);
}

/** First 12 hex characters, the length docker prints. */
export function shortDigest(d: string): string {
	const hex = d.includes(':') ? d.slice(d.indexOf(':') + 1) : d;
	return hex.slice(0, 12);
}

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });
const steps: [Intl.RelativeTimeFormatUnit, number][] = [
	['year', 31536000],
	['month', 2592000],
	['week', 604800],
	['day', 86400],
	['hour', 3600],
	['minute', 60]
];

/** "3 hours ago" for a unix timestamp in seconds. */
export function ago(unix: number, now = Date.now() / 1000): string {
	if (!unix) return 'never';
	const diff = unix - now;
	for (const [unit, secs] of steps) {
		if (Math.abs(diff) >= secs) return rtf.format(Math.round(diff / secs), unit);
	}
	return 'just now';
}

const dtf = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });

/** Absolute local time, for tooltips next to relative times. */
export function when(unix: number): string {
	return unix ? dtf.format(new Date(unix * 1000)) : '';
}

export interface Platform {
	os: string;
	architecture: string;
	variant?: string;
}

export function platform(p: Platform | null | undefined): string {
	if (!p) return 'unknown';
	return [p.os, p.architecture, p.variant].filter(Boolean).join('/');
}

/** Split a UI path like "team/app:v1" or "team/app@sha256:…" into repo and reference. */
export function parseRef(path: string): { repo: string; ref: string } {
	const at = path.indexOf('@');
	if (at >= 0) return { repo: path.slice(0, at), ref: path.slice(at + 1) };
	const slash = path.lastIndexOf('/');
	const colon = path.indexOf(':', slash + 1);
	if (colon >= 0) return { repo: path.slice(0, colon), ref: path.slice(colon + 1) };
	return { repo: path, ref: '' };
}

/** The UI path of a manifest. */
export function refPath(repo: string, ref: string): string {
	return `/r/${repo}${ref.includes(':') ? '@' : ':'}${ref}`;
}

/** A same-origin path to return to after sign-in; anything else falls back to "/". */
export function safeNext(next: string | null | undefined): string {
	return next && next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/\\')
		? next
		: '/';
}

/** Share of `total` as a 0–1 fraction, safe for a zero or missing total. */
export function fraction(part: number, total: number): number {
	if (!total || total <= 0 || !Number.isFinite(part)) return 0;
	return Math.min(1, Math.max(0, part / total));
}

/** Count with a pluralised noun: plural(1, 'tag') → "1 tag", plural(3, 'tag') → "3 tags". */
export function plural(n: number, noun: string, many = noun + 's'): string {
	return `${n.toLocaleString('en')} ${n === 1 ? noun : many}`;
}

/** Readable media type: "application/vnd.oci.image.index.v1+json" → "OCI image index". */
export function mediaTypeLabel(mt: string): string {
	const known: Record<string, string> = {
		'application/vnd.oci.image.index.v1+json': 'OCI image index',
		'application/vnd.oci.image.manifest.v1+json': 'OCI image manifest',
		'application/vnd.docker.distribution.manifest.list.v2+json': 'Docker manifest list',
		'application/vnd.docker.distribution.manifest.v2+json': 'Docker image manifest'
	};
	return known[mt] ?? mt.replace(/^application\/vnd\./, '').replace(/\+json$/, '');
}

/** Docker history "created_by" without the shell wrapper noise. */
export function historyCommand(createdBy: string | undefined): string {
	return (createdBy ?? '')
		.replace(/^\/bin\/sh -c #\(nop\)\s+/, '')
		.replace(/^\/bin\/sh -c /, 'RUN ')
		.trim();
}

/**
 * The HEALTHCHECK test as written in the Dockerfile: shell form as plain text,
 * exec form as a JSON array. Empty when the image inherits its base's check.
 */
export function healthcheckCommand(test: string[] | undefined): string {
	if (!test?.length) return '';
	const [kind, ...args] = test;
	if (kind === 'NONE') return 'disabled';
	if (kind === 'CMD-SHELL') return args.join(' ');
	if (kind === 'CMD') return JSON.stringify(args);
	return JSON.stringify(test);
}

/** Go-style duration ("1m30s", "500ms") from nanoseconds. */
export function duration(ns: number): string {
	if (ns <= 0) return '0s';
	if (ns < 1e9) return `${Math.round(ns / 1e6)}ms`;
	let s = Math.round(ns / 1e9);
	const h = Math.floor(s / 3600);
	s -= h * 3600;
	const m = Math.floor(s / 60);
	s -= m * 60;
	return (h ? `${h}h` : '') + (m ? `${m}m` : '') + (s || (!h && !m) ? `${s}s` : '');
}

/** The HEALTHCHECK options that differ from Docker's defaults, in Dockerfile order. */
export function healthcheckTiming(h: {
	Interval?: number;
	Timeout?: number;
	StartPeriod?: number;
	StartInterval?: number;
	Retries?: number;
}): string {
	const parts = [];
	if (h.Interval) parts.push(`every ${duration(h.Interval)}`);
	if (h.Timeout) parts.push(`timeout ${duration(h.Timeout)}`);
	if (h.StartPeriod) parts.push(`start period ${duration(h.StartPeriod)}`);
	if (h.StartInterval) parts.push(`start interval ${duration(h.StartInterval)}`);
	if (h.Retries) parts.push(plural(h.Retries, 'retry', 'retries'));
	return parts.join(', ');
}

/** One-line summary of a retention rule: "keep the newest 10, delete the rest". */
export function describePolicy(p: { keepLast: number; olderThanDays: number }): string {
	const parts = [];
	if (p.keepLast) parts.push(`keep the newest ${p.keepLast}`);
	if (p.olderThanDays)
		parts.push(
			`${p.keepLast ? 'delete the rest' : 'delete tags'} older than ${p.olderThanDays} days`
		);
	else if (p.keepLast) parts.push('delete the rest');
	return parts.join(', ') || 'no limits set';
}

/** What copy buttons for images put on the clipboard. */
export type CopyMode = 'ref' | 'command';

/**
 * Clipboard text for an image: the bare reference ("host/repo:tag") or the full pull
 * command. The platform flag only exists in command form.
 */
export function pullText(ref: string, mode: CopyMode, platform?: string): string {
	if (mode === 'ref') return ref;
	return platform ? `docker pull --platform ${platform} ${ref}` : `docker pull ${ref}`;
}

/**
 * An image reference split before its last path segment, so a cramped display can cut the
 * host and namespace and keep "name:tag" readable. Tags and digests never contain a slash.
 */
export function splitReference(ref: string): { head: string; tail: string } {
	const slash = ref.lastIndexOf('/');
	return { head: ref.slice(0, slash + 1), tail: ref.slice(slash + 1) };
}

/** The project a repository belongs to: its first path segment, "library" without one. */
export function projectOf(repo: string): string {
	const slash = repo.indexOf('/');
	return slash < 0 ? 'library' : repo.slice(0, slash);
}

/** Login name for docker; robot names contain "$", so they are single-quoted for the shell. */
export function shellQuote(s: string): string {
	return /^[A-Za-z0-9@%+=:,./_-]+$/.test(s) ? s : `'${s.replaceAll("'", `'\\''`)}'`;
}

/** Humanised audit action: "delete_tag" → "Delete tag". */
export function actionLabel(action: string): string {
	const s = action.replaceAll('_', ' ');
	return s.charAt(0).toUpperCase() + s.slice(1);
}

/** Unix seconds for the end of a local "YYYY-MM-DD" day, 0 for an empty value. */
export function dateToUnix(date: string): number {
	if (!date) return 0;
	const [y, m, d] = date.split('-').map(Number);
	return Math.floor(new Date(y, m - 1, d, 23, 59, 59).getTime() / 1000);
}

/** Local "YYYY-MM-DD" for a unix time, "" for 0. */
export function unixToDate(unix: number): string {
	if (!unix) return '';
	const d = new Date(unix * 1000);
	const pad = (n: number) => String(n).padStart(2, '0');
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Quota field value in GB (decimal, like the sizes shown) to bytes; empty or 0 means no quota. */
export function gbToBytes(gb: number | string | null | undefined): number {
	const n = Number(gb);
	return Number.isFinite(n) && n > 0 ? Math.round(n * 1e9) : 0;
}

/** A device code as typed by a person: uppercase, the dash restored after four characters. */
export function normalizeUserCode(input: string): string {
	const raw = input
		.toUpperCase()
		.replace(/[^A-Z0-9]/g, '')
		.slice(0, 8);
	return raw.length > 4 ? `${raw.slice(0, 4)}-${raw.slice(4)}` : raw;
}
