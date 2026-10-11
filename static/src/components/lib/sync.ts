/**
 * Runs a refetch one at a time and keeps asking for it until it has worked. Requests that arrive
 * while it runs are merged into one more run; a run that fails is retried after a backoff (the
 * requests that arrived meanwhile are not dropped, the retry covers them), and once the retries
 * are used up the next request starts over.
 */
import { createLogger } from './logger';

export const SYNC_MAX_RETRIES = 5;

export function syncRetryDelay(attempt: number): number {
  return Math.min(10_000, 1000 * 2 ** attempt);
}

export interface SyncerOptions {
  /** One refetch. Rejecting means it failed and is to be tried again. */
  run: () => Promise<unknown>;
  /** Called when the retries are used up. */
  onGiveUp?: () => void;
  maxRetries?: number;
  retryDelay?: (attempt: number) => number;
  setTimer?: (fn: () => void, ms: number) => unknown;
  clearTimer?: (handle: unknown) => void;
}

export interface Syncer {
  /** Asks for a refetch. Resolves when nothing is left to do for now; never rejects. */
  request(): Promise<void>;
  readonly syncing: boolean;
}

const log = createLogger('sync');

export function createSyncer(options: SyncerOptions): Syncer {
  const maxRetries = options.maxRetries ?? SYNC_MAX_RETRIES;
  const retryDelay = options.retryDelay ?? syncRetryDelay;
  const setTimer = options.setTimer ?? ((fn: () => void, ms: number) => setTimeout(fn, ms));
  const clearTimer = options.clearTimer ?? ((h: unknown) => clearTimeout(h as ReturnType<typeof setTimeout>));

  let syncing = false;
  let again = false;
  let failures = 0;
  let timer: unknown = null;

  const start = async (retry: boolean): Promise<void> => {
    if (timer !== null) {
      clearTimer(timer);
      timer = null;
    }
    if (!retry) failures = 0;
    if (syncing) {
      again = true;
      return;
    }
    syncing = true;
    try {
      do {
        again = false;
        try {
          await options.run();
          failures = 0;
        } catch (error) {
          failures += 1;
          log.warn(`refetch failed (${failures})`, error);
          if (failures > maxRetries) {
            options.onGiveUp?.();
          } else {
            timer = setTimer(
              () => {
                timer = null;
                void start(true);
              },
              retryDelay(failures - 1),
            );
          }
          return;
        }
      } while (again);
    } finally {
      syncing = false;
    }
  };

  return {
    request: () => start(false),
    get syncing() {
      return syncing;
    },
  };
}
