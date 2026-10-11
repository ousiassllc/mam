import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, CSRF_HEADER, csrfToken, get, post } from './api';

function stubDocument(token: string | null) {
  vi.stubGlobal('document', {
    querySelector: (selector: string) =>
      selector === 'meta[name="csrf-token"]' && token !== null ? { content: token } : null,
  });
}

function stubFetch(status: number, body: string) {
  const fetchMock = vi.fn(async () => new Response(body === '' ? null : body, { status }));
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function lastCall(mock: ReturnType<typeof stubFetch>) {
  const call = mock.mock.calls[0] as unknown as [string, RequestInit];
  return { url: call[0], init: call[1], headers: call[1].headers as Record<string, string> };
}

describe('api client', () => {
  beforeEach(() => stubDocument('tok-123'));
  afterEach(() => vi.unstubAllGlobals());

  it('reads the CSRF token from the meta tag and tolerates its absence', () => {
    expect(csrfToken()).toBe('tok-123');
    stubDocument(null);
    expect(csrfToken()).toBe('');
  });

  it('sends the CSRF token on every method, including GET', async () => {
    const fetchMock = stubFetch(200, '{"ok":true}');
    await expect(get('/api/v1/health')).resolves.toEqual({ ok: true });
    expect(lastCall(fetchMock).headers[CSRF_HEADER]).toBe('tok-123');
  });

  it('serialises JSON bodies with a JSON content type', async () => {
    const fetchMock = stubFetch(200, '{}');
    await post('/api/v1/shots/1/generate', { count: 2 });
    const { init, headers } = lastCall(fetchMock);
    expect(init.method).toBe('POST');
    expect(init.body).toBe('{"count":2}');
    expect(headers['Content-Type']).toBe('application/json');
    expect(headers[CSRF_HEADER]).toBe('tok-123');
  });

  it('throws ApiError with the status and parsed body on failure', async () => {
    stubFetch(403, '{"error":"invalid or missing CSRF token"}');
    const error = await get('/api/v1/x').catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(403);
    expect((error as ApiError).body).toEqual({ error: 'invalid or missing CSRF token' });
  });

  it('resolves undefined for empty responses', async () => {
    stubFetch(204, '');
    await expect(get('/api/v1/x')).resolves.toBeUndefined();
  });
});
