import type { CopyMode } from './format';

const key = 'doreg.copyMode';

function initial(): CopyMode {
	try {
		return localStorage.getItem(key) === 'command' ? 'command' : 'ref';
	} catch {
		return 'ref';
	}
}

/** Per-browser UI preferences, kept in localStorage. */
export const prefs = $state<{ copyMode: CopyMode }>({ copyMode: initial() });

export function setCopyMode(mode: CopyMode) {
	prefs.copyMode = mode;
	try {
		localStorage.setItem(key, mode);
	} catch {
		/* private mode: the choice lasts for this tab only */
	}
}
