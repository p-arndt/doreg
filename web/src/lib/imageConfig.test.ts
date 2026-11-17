import { describe, expect, it } from 'vitest';
import { envItems, preferredImage, runtimeItems } from './imageConfig.js';
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
