/** Test doubles for the network the Lit components use: fetch (lib/api) and WebSocket (lib/ws). */

export interface RecordedRequest {
  method: string;
  url: string;
  body: unknown;
  headers: Record<string, string>;
}

type Reply = { status?: number; body?: unknown };

/**
 * Replaces window.fetch. `routes` maps "METHOD url" to a reply (or a function of the request);
 * anything else answers 404. Returns the recorded requests and a restore function.
 */
export function stubFetch(routes: Record<string, Reply | ((req: RecordedRequest) => Reply)>) {
  const original = window.fetch;
  const requests: RecordedRequest[] = [];
  window.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const req: RecordedRequest = {
      method: init?.method ?? 'GET',
      url: String(input),
      body: init?.body ? tryParse(String(init.body)) : undefined,
      headers: (init?.headers ?? {}) as Record<string, string>,
    };
    requests.push(req);
    const route = routes[`${req.method} ${req.url}`];
    const reply = typeof route === 'function' ? route(req) : (route ?? { status: 404, body: { title: 'Not Found' } });
    const text = reply.body === undefined ? '' : typeof reply.body === 'string' ? reply.body : JSON.stringify(reply.body);
    return new Response(text, { status: reply.status ?? 200 });
  }) as typeof window.fetch;
  return {
    requests,
    restore: () => {
      window.fetch = original;
    },
  };
}

function tryParse(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

/** A WebSocket the test drives by hand. */
export class FakeSocket {
  static instances: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  closed = false;

  constructor(readonly url: string) {
    FakeSocket.instances.push(this);
  }

  close(): void {
    this.closed = true;
  }

  /** Delivers a message from the server. */
  emit(message: unknown): void {
    this.onmessage?.({ data: JSON.stringify(message) });
  }
}

/** Replaces window.WebSocket with FakeSocket; returns the restore function. */
export function stubWebSocket(): () => void {
  const original = window.WebSocket;
  FakeSocket.instances = [];
  window.WebSocket = FakeSocket as unknown as typeof WebSocket;
  return () => {
    window.WebSocket = original;
  };
}

/**
 * Puts #connection-notice-template (the markup of atoms/connection_notice.templ, which AppLayout
 * embeds) into the document, as the page shell does. Returns a function that removes it and the
 * notice region.
 */
export function installConnectionNoticeTemplate(): () => void {
  const template = document.createElement('template');
  template.id = 'connection-notice-template';
  template.innerHTML =
    '<div class="notice" role="alert" data-connection-lost=""><span data-connection-lost-message></span><button type="button" data-action="reconnect">再接続</button><button type="button" data-action="reload">再読み込み</button></div>';
  document.body.append(template);
  return () => {
    template.remove();
    document.getElementById('connection-lost-region')?.remove();
  };
}
