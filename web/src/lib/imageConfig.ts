import { healthcheckCommand, healthcheckTiming, platform } from './format.js';
import type { Platform } from './format.js';
import type { ImageConfig } from './types.js';

/** The runtime settings of an image config as label/value rows, in reading order. */
export function runtimeItems(config: ImageConfig): [string, string][] {
	const cfg = config.config;
	const items: [string, string][] = [['Platform', platform(config)]];
	if (cfg?.Entrypoint) items.push(['Entrypoint', JSON.stringify(cfg.Entrypoint)]);
	if (cfg?.Cmd) items.push(['Command', JSON.stringify(cfg.Cmd)]);
	if (cfg?.WorkingDir) items.push(['Working dir', cfg.WorkingDir]);
	if (cfg?.User) items.push(['User', cfg.User]);
	if (cfg?.ExposedPorts) items.push(['Ports', Object.keys(cfg.ExposedPorts).join(', ')]);
	if (cfg?.Volumes) items.push(['Volumes', Object.keys(cfg.Volumes).join(', ')]);
	if (cfg?.StopSignal) items.push(['Stop signal', cfg.StopSignal]);
	const check = healthcheckCommand(cfg?.Healthcheck?.Test);
	if (check) {
		items.push(['Healthcheck', check]);
		const timing = check === 'disabled' ? '' : healthcheckTiming(cfg!.Healthcheck!);
		if (timing) items.push(['Health timing', timing]);
	}
	if (cfg?.Shell) items.push(['Shell', JSON.stringify(cfg.Shell)]);
	if (cfg?.OnBuild?.length) items.push(['On build', cfg.OnBuild.join(' · ')]);
	if (config.author) items.push(['Author', config.author]);
	return items;
}

/** Environment variables split at the first "=". */
export function envItems(config: ImageConfig): [string, string][] {
	return (config.config?.Env ?? []).map((e) => {
		const i = e.indexOf('=');
		return i < 0 ? [e, ''] : [e.slice(0, i), e.slice(i + 1)];
	});
}

/**
 * The image of an index to show first: linux/amd64 is what most people run, then any linux
 * image, then whatever comes first.
 */
export function preferredImage<T extends { platform?: Platform }>(images: T[]): T | undefined {
	return (
		images.find((i) => i.platform?.os === 'linux' && i.platform.architecture === 'amd64') ??
		images.find((i) => i.platform?.os === 'linux') ??
		images[0]
	);
}

/** The most recently pushed tag; on equal push times the later name in natural order wins. */
export function latestTag<T extends { name: string; updatedAt: number }>(tags: T[]): T | undefined {
	let best: T | undefined;
	for (const t of tags) {
		if (
			!best ||
			t.updatedAt > best.updatedAt ||
			(t.updatedAt === best.updatedAt &&
				t.name.localeCompare(best.name, undefined, { numeric: true }) > 0)
		)
			best = t;
	}
	return best;
}

const indexTypes = new Set([
	'application/vnd.oci.image.index.v1+json',
	'application/vnd.docker.distribution.manifest.list.v2+json'
]);

/**
 * The manifest whose config describes a pushed image: the manifest itself, or for an index the
 * platform image people most likely run. "" when there is none (e.g. an index of artifacts).
 */
export function configSource(m: {
	digest: string;
	mediaType: string;
	platforms: (Platform & { digest: string })[];
}): string {
	if (!indexTypes.has(m.mediaType)) return m.digest;
	return preferredImage(m.platforms.map((p) => ({ platform: p, digest: p.digest })))?.digest ?? '';
}
