import { describe, expect, it, vi } from 'vitest';
import { MAX_RETRIES, STABLE_MS, backoffDelay, connect, socketUrl, type SocketLike } from './ws';

class FakeSocket implements SocketLike {
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  closed = false;
  close() {
    this.closed = true;
  }
}

function harness() {
  const sockets: FakeSocket[] = [];
  const timers: { fn: () => void; ms: number }[] = [];
  const clock = { now: 0 };
  const resumers: (() => void)[] = [];
  const options = {
    url: 'ws://x/ws',
    now: () => clock.now,
    subscribeResume: (resume: () => void) => {
      resumers.push(resume);
      return () => resumers.splice(resumers.indexOf(resume), 1);
    },
    createSocket: () => {
      const s = new FakeSocket();
      sockets.push(s);
      return s;
    },
    setTimer: (fn: () => void, ms: number) => {
      timers.push({ fn, ms });
      return timers.length;
    },
    clearTimer: () => timers.splice(0),
  };
  return { sockets, timers, options, clock, resumers };
}

describe('backoffDelay', () => {
  it('doubles per attempt and is capped', () => {
    expect([0, 1, 2, 3].map(backoffDelay)).toEqual([500, 1000, 2000, 4000]);
    expect(backoffDelay(20)).toBe(30_000);
  });
});

describe('socketUrl', () => {
  it('follows the page protocol', () => {
    expect(socketUrl('/ws/projects/1', { protocol: 'http:', host: '127.0.0.1:8787' })).toBe('ws://127.0.0.1:8787/ws/projects/1');
    expect(socketUrl('/ws/projects/1', { protocol: 'https:', host: 'h' })).toBe('wss://h/ws/projects/1');
  });
});

describe('connect', () => {
  it('delivers parsed messages and ignores malformed ones', () => {
    const h = harness();
    const onMessage = vi.fn();
    connect({ ...h.options, onMessage });
    h.sockets[0]!.onmessage?.({ data: '{"type":"message.created","message_id":"m1"}' });
    h.sockets[0]!.onmessage?.({ data: 'not json' });
    expect(onMessage).toHaveBeenCalledTimes(1);
    expect(onMessage).toHaveBeenCalledWith({ type: 'message.created', message_id: 'm1' });
  });

  it('reconnects with growing delays and reports reconnection to onOpen', () => {
    const h = harness();
    const onOpen = vi.fn();
    connect({ ...h.options, onMessage: vi.fn(), onOpen });
    h.sockets[0]!.onopen?.();
    h.clock.now += STABLE_MS; // a working connection
    h.sockets[0]!.onclose?.();
    expect(h.timers.map((t) => t.ms)).toEqual([500]);
    h.timers[0]!.fn();
    h.sockets[1]!.onclose?.(); // failed to connect
    expect(h.timers.map((t) => t.ms)).toEqual([500, 500]);
    h.timers[1]!.fn();
    h.sockets[2]!.onclose?.();
    expect(h.timers[2]!.ms).toBe(1000);
    h.timers[2]!.fn();
    h.sockets[3]!.onopen?.();
    expect(onOpen.mock.calls).toEqual([[false], [true]]);
  });

  it('gives up after MAX_RETRIES consecutive failures', () => {
    const h = harness();
    const onGiveUp = vi.fn();
    connect({ ...h.options, onMessage: vi.fn(), onGiveUp });
    for (let i = 0; i < MAX_RETRIES; i++) {
      h.sockets[i]!.onclose?.();
      if (i < MAX_RETRIES - 1) h.timers[i]!.fn();
    }
    expect(h.sockets).toHaveLength(MAX_RETRIES);
    expect(onGiveUp).toHaveBeenCalledTimes(1);
    expect(h.timers).toHaveLength(MAX_RETRIES - 1);
  });

  it('does not reconnect after close()', () => {
    const h = harness();
    const conn = connect({ ...h.options, onMessage: vi.fn() });
    conn.close();
    h.sockets[0]!.onclose?.();
    expect(h.sockets[0]!.closed).toBe(true);
    expect(h.timers).toHaveLength(0);
    expect(h.resumers).toHaveLength(0);
  });

  it('keeps backing off and gives up when the server accepts and hangs up again at once', () => {
    const h = harness();
    const onGiveUp = vi.fn();
    connect({ ...h.options, onMessage: vi.fn(), onGiveUp });
    for (let i = 0; i < MAX_RETRIES; i++) {
      h.sockets[i]!.onopen?.();
      h.clock.now += STABLE_MS - 1;
      h.sockets[i]!.onclose?.();
      if (i < MAX_RETRIES - 1) h.timers[i]!.fn();
    }
    expect(h.timers.map((t) => t.ms)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8].map(backoffDelay));
    expect(onGiveUp).toHaveBeenCalledTimes(1);
    expect(h.sockets).toHaveLength(MAX_RETRIES);
  });

  it('starts over with a short delay once a connection has lived long enough', () => {
    const h = harness();
    connect({ ...h.options, onMessage: vi.fn() });
    for (let i = 0; i < 3; i++) {
      h.sockets[i]!.onclose?.(); // never opened
      h.timers[i]!.fn();
    }
    h.sockets[3]!.onopen?.();
    h.clock.now += STABLE_MS;
    h.sockets[3]!.onclose?.();
    expect(h.timers.at(-1)!.ms).toBe(backoffDelay(0));
  });

  it('retries at once after giving up when the page resumes (online / visible) or retry() is called', () => {
    const h = harness();
    const onOpen = vi.fn();
    const conn = connect({ ...h.options, onMessage: vi.fn(), onOpen, onGiveUp: vi.fn() });
    for (let i = 0; i < MAX_RETRIES; i++) {
      h.sockets[i]!.onclose?.();
      if (i < MAX_RETRIES - 1) h.timers[i]!.fn();
    }
    expect(h.sockets).toHaveLength(MAX_RETRIES);

    h.resumers[0]!();
    expect(h.sockets).toHaveLength(MAX_RETRIES + 1);
    h.sockets[MAX_RETRIES]!.onopen?.();
    expect(onOpen).toHaveBeenCalledWith(false);

    h.resumers[0]!(); // connected: nothing to do
    conn.retry();
    expect(h.sockets).toHaveLength(MAX_RETRIES + 1);

    h.clock.now += STABLE_MS;
    h.sockets[MAX_RETRIES]!.onclose?.();
    expect(h.timers).toHaveLength(1);
    conn.retry(); // skips the backoff
    expect(h.sockets).toHaveLength(MAX_RETRIES + 2);
    expect(h.timers).toHaveLength(0); // the waiting timer was cancelled
  });

  it('does not open a second socket when resumed while an attempt is under way', () => {
    const h = harness();
    connect({ ...h.options, onMessage: vi.fn() });
    h.resumers[0]!();
    expect(h.sockets).toHaveLength(1);
  });
});
