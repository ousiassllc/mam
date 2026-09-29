/**
 * WebSocket client with automatic reconnection (exponential backoff, at most
 * MAX_RETRIES consecutive failed attempts). Messages are JSON.
 */
import { createLogger } from './logger';

export const MAX_RETRIES = 10;
const BASE_DELAY_MS = 500;
const MAX_DELAY_MS = 30_000;

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
  /** Called on every successful connection; `reconnected` is false for the first one. */
  onOpen?: (reconnected: boolean) => void;
  /** Called once the retries are exhausted. */
  onGiveUp?: () => void;
  createSocket?: (url: string) => SocketLike;
  setTimer?: (fn: () => void, ms: number) => unknown;
  clearTimer?: (handle: unknown) => void;
}

export interface Connection {
  close(): void;
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

  let socket: SocketLike | null = null;
  let timer: unknown = null;
  let failures = 0;
  let opened = false;
  let closed = false;

  const open = () => {
    const current = createSocket(options.url);
    socket = current;
    let connected = false;
    current.onopen = () => {
      connected = true;
      failures = 0;
      options.onOpen?.(opened);
      opened = true;
    };
    current.onmessage = (event) => {
      try {
        options.onMessage(JSON.parse(String(event.data)) as T);
      } catch (error) {
        log.warn('ignored a malformed message', error);
      }
    };
    current.onclose = () => {
      if (closed || socket !== current) return;
      if (!connected) failures += 1;
      if (failures >= MAX_RETRIES) {
        log.warn('gave up reconnecting');
        options.onGiveUp?.();
        return;
      }
      timer = setTimer(open, backoffDelay(connected ? 0 : failures - 1));
    };
  };

  open();
  return {
    close() {
      closed = true;
      if (timer !== null) clearTimer(timer);
      socket?.close();
    },
  };
}
