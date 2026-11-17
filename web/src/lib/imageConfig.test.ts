import { describe, expect, it } from 'vitest';
import { configSource, envItems, latestTag, preferredImage, runtimeItems } from './imageConfig.js';
import type { ImageConfig } from './types.js';

const config: ImageConfig = {
	os: 'linux',
	architecture: 'amd64',
	config: {
		Entrypoint: ['/doreg'],
		Cmd: ['serve'],
		User: '65532',
		ExposedPorts: { '8080/tcp': {} },
		Env: ['PATH=/usr/bin', 'EMPTY', 'A=b=c']
	}
};

describe('runtimeItems', () => {
	it('lists set fields in order', () => {
		expect(runtimeItems(config)).toEqual([
			['Platform', 'linux/amd64'],
			['Entrypoint', '["/doreg"]'],
			['Command', '["serve"]'],
			['User', '65532'],
			['Ports', '8080/tcp']
		]);
	});
});

describe('envItems', () => {
	it('splits at the first equals sign', () => {
		expect(envItems(config)).toEqual([
			['PATH', '/usr/bin'],
			['EMPTY', ''],
			['A', 'b=c']
		]);
	});
});

describe('preferredImage', () => {
	const img = (os: string, architecture: string) => ({ platform: { os, architecture } });

	it('prefers linux/amd64', () => {
		const images = [img('linux', 'arm64'), img('linux', 'amd64')];
		expect(preferredImage(images)).toBe(images[1]);
	});

	it('falls back to any linux image, then the first', () => {
		const linux = [img('windows', 'amd64'), img('linux', 'arm64')];
		expect(preferredImage(linux)).toBe(linux[1]);
		const other = [img('windows', 'amd64')];
		expect(preferredImage(other)).toBe(other[0]);
		expect(preferredImage([])).toBeUndefined();
	});
});

describe('latestTag', () => {
	it('picks the most recent push', () => {
		const tags = [
			{ name: '0.8.0', updatedAt: 10 },
			{ name: '0.9.0', updatedAt: 30 },
			{ name: '0.2.0', updatedAt: 20 }
		];
		expect(latestTag(tags)?.name).toBe('0.9.0');
	});

	it('breaks ties by natural name order', () => {
		const tags = [
			{ name: '0.9.0', updatedAt: 5 },
			{ name: '0.10.0', updatedAt: 5 },
			{ name: 'latest', updatedAt: 1 }
		];
		expect(latestTag(tags)?.name).toBe('0.10.0');
	});

	it('returns undefined without tags', () => {
		expect(latestTag([])).toBeUndefined();
	});
});

describe('configSource', () => {
	it('uses the manifest itself for a single image', () => {
		expect(
			configSource({
				digest: 'sha256:a',
				mediaType: 'application/vnd.oci.image.manifest.v1+json',
				platforms: [{ os: 'linux', architecture: 'amd64', digest: 'sha256:a' }]
			})
		).toBe('sha256:a');
	});

	it('picks linux/amd64 from an index', () => {
		expect(
			configSource({
				digest: 'sha256:i',
				mediaType: 'application/vnd.oci.image.index.v1+json',
				platforms: [
					{ os: 'linux', architecture: 'arm64', digest: 'sha256:arm' },
					{ os: 'linux', architecture: 'amd64', digest: 'sha256:amd' }
				]
			})
		).toBe('sha256:amd');
	});

	it('is empty for an index without images', () => {
		expect(
			configSource({
				digest: 'sha256:i',
				mediaType: 'application/vnd.docker.distribution.manifest.list.v2+json',
				platforms: []
			})
		).toBe('');
	});
});
