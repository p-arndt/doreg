export interface Toast {
	id: number;
	message: string;
	tone: 'success' | 'danger';
}

export const toasts = $state<Toast[]>([]);
let nextId = 1;

export function dismiss(id: number) {
	const i = toasts.findIndex((t) => t.id === id);
	if (i >= 0) toasts.splice(i, 1);
}

export function notify(message: string, tone: Toast['tone'] = 'success') {
	const id = nextId++;
	toasts.push({ id, message, tone });
	// Errors stay longer: they usually need reading.
	setTimeout(() => dismiss(id), tone === 'danger' ? 8000 : 4000);
}

export function fail(e: unknown) {
	notify(e instanceof Error ? e.message : String(e), 'danger');
}
