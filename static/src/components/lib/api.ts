/**
 * HTTP client for the /api/v1 JSON API. Lit components MUST use this instead of
 * calling fetch() directly so that every request carries the CSRF token.
 */

export const CSRF_HEADER = 'X-CSRF-Token';

/** Reads the token the server renders into <meta name="csrf-token">. */
export function csrfToken(): string {
  const meta = document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]');
  return meta?.content ?? '';
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly body: unknown,
  ) {
    super(`API request failed with status ${status}`);
    this.name = 'ApiError';
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'DELETE';

async function request<T>(method: Method, url: string, body?: string | null, json = false): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json', [CSRF_HEADER]: csrfToken() };
  if (json) headers['Content-Type'] = 'application/json';

  const response = await fetch(url, { method, headers, body, credentials: 'same-origin' });
  const text = await response.text();
  const parsed: unknown = text ? safeParse(text) : undefined;
  if (!response.ok) throw new ApiError(response.status, parsed);
  return parsed as T;
}

function safeParse(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

export const get = <T>(url: string) => request<T>('GET', url);
export const post = <T>(url: string, data?: unknown) =>
  request<T>('POST', url, data === undefined ? null : JSON.stringify(data), data !== undefined);
export const put = <T>(url: string, data?: unknown) =>
  request<T>('PUT', url, data === undefined ? null : JSON.stringify(data), data !== undefined);
export const del = <T>(url: string) => request<T>('DELETE', url);
