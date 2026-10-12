/**
 * WebSocket client with automatic reconnection (exponential backoff, at most MAX_RETRIES
 * consecutive failed attempts). Messages are JSON.
 *
 * A connection that closes within STABLE_MS of opening counts as a failed attempt, like one that
 * never opened: a server that accepts and hangs up again is given up on instead of being retried
 * every 500 ms for ever. After giving up, and while waiting for the next attempt, the page coming
 * back online or visible tries again at once, as does `retry()`.
 */
import { createLogger } from './logger';

export const MAX_RETRIES = 10;
/** What the user is told when the retries are used up (the caller adds what stopped updating). */
export const CONNECTION_LOST_MESSAGE = '接続が切れました。';
const BASE_DELAY_MS = 500;
const MAX_DELAY_MS = 30_000;
/** A connection that lived this long was a working one. */
export const STABLE_MS = 1000;

/** The part of WebSocket this module uses; tests inject a fake. */
export interface SocketLike {
  onopen: (() => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: (() => void) | null;
  close(): void;
}

export interface ConnectOptions<T> {
  url: string;
  onMessage: (message: T) => void;
  /** Called on every successful connection; `reconnected` is false only for the very first attempt: any connection that follows an earlier attempt (a failed one included) may have missed events the caller has to catch up on. */
  onOpen?: (reconnected: boolean) => void;
  /** Called once the retries are exhausted. */
  onGiveUp?: () => void;
  createSocket?: (url: string) => SocketLike;
  setTimer?: (fn: () => void, ms: number) => unknown;
  clearTimer?: (handle: unknown) => void;
  now?: () => number;
  /** Registers `resume` for "the page may have a network / the user is looking again"; returns the unregister function. Defaults to window `online` and document `visibilitychange`. */
  subscribeResume?: (resume: () => void) => () => void;
}

export interface Connection {
  /** Connects now when waiting for the next attempt or given up, forgetting earlier failures. */
  retry(): void;
  close(): void;
}

function browserResume(resume: () => void): () => void {
  if (typeof window === 'undefined' || typeof document === 'undefined') return () => {};
  const onVisible = () => {
    if (document.visibilityState === 'visible') resume();
  };
  window.addEventListener('online', resume);
  document.addEventListener('visibilitychange', onVisible);
  return () => {
    window.removeEventListener('online', resume);
    document.removeEventListener('visibilitychange', onVisible);
  };
}

/** Delay before reconnect attempt `attempt` (0-based): 0.5s, 1s, 2s, ... capped at 30s. */
export function backoffDelay(attempt: number): number {
  return Math.min(MAX_DELAY_MS, BASE_DELAY_MS * 2 ** attempt);
}

/** ws://host/path for a same-origin path such as /ws/projects/1. */
export function socketUrl(path: string, location: { protocol: string; host: string }): string {
  return `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}${path}`;
}

const log = createLogger('ws');

export function connect<T>(options: ConnectOptions<T>): Connection {
  const createSocket = options.createSocket ?? ((url: string) => new WebSocket(url) as unknown as SocketLike);
  const setTimer = options.setTimer ?? ((fn: () => void, ms: number) => setTimeout(fn, ms));
  const clearTimer = options.clearTimer ?? ((h: unknown) => clearTimeout(h as ReturnType<typeof setTimeout>));
  const now = options.now ?? Date.now;

  let socket: SocketLike | null = null;
  let timer: unknown = null;
  let failures = 0;
  let attempted = false; // has any socket been created before (it opened and dropped, or never opened)?
  let closed = false;
  let waiting = false; // no socket: waiting for the next attempt, or given up

  const open = () => {
    waiting = false;
    const reconnected = attempted;
    attempted = true;
    const current = createSocket(options.url);
    socket = current;
    let openedAt: number | null = null;
    current.onopen = () => {
      openedAt = now();
      options.onOpen?.(reconnected);
    };
    current.onmessage = (event) => {
      let message: T | undefined;
      try {
        message = JSON.parse(String(event.data)) as T;
      } catch (error) {
        log.warn('ignored a malformed message', error);
      }
      if (message !== undefined) options.onMessage(message);
    };
    current.onclose = () => {
      if (closed || socket !== current) return;
      waiting = true;
      failures = openedAt !== null && now() - openedAt >= STABLE_MS ? 0 : failures + 1;
      if (failures >= MAX_RETRIES) {
        log.warn('gave up reconnecting');
        options.onGiveUp?.();
        return;
      }
      timer = setTimer(open, backoffDelay(Math.max(failures - 1, 0)));
    };
  };

  const retry = () => {
    if (closed || !waiting) return;
    if (timer !== null) clearTimer(timer);
    timer = null;
    failures = 0;
    open();
  };

  const unsubscribe = (options.subscribeResume ?? browserResume)(retry);
  open();
  return {
    retry,
    close() {
      closed = true;
      unsubscribe();
      if (timer !== null) clearTimer(timer);
      socket?.close();
    },
  };
}
