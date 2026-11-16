import { goto } from '$app/navigation';

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string
	) {
		super(message);
	}
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const res = await fetch(path, {
		method,
		headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
		body: body === undefined ? undefined : JSON.stringify(body),
		credentials: 'same-origin'
	});
	if (res.status === 401 && path !== '/api/login') {
		const back = location.pathname + location.search;
		await goto(`/login?next=${encodeURIComponent(back)}`);
		throw new ApiError(401, 'Please sign in.');
	}
	if (!res.ok) {
		let msg = res.statusText;
		try {
			msg = (await res.json()).error ?? msg;
		} catch {
			/* body was not JSON */
		}
		throw new ApiError(res.status, msg);
	}
	if (res.status === 204 || res.headers.get('Content-Length') === '0') return undefined as T;
	return res.json();
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
	put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
	patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body ?? {}),
	del: <T>(path: string) => request<T>('DELETE', path)
};

export const q = encodeURIComponent;
