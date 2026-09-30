import { describe, expect, it, vi } from 'vitest';
import { createSyncer, syncRetryDelay } from './sync';

function timers() {
  const pending: { fn: () => void; ms: number }[] = [];
  return {
    pending,
    setTimer: (fn: () => void, ms: number) => {
      pending.push({ fn, ms });
      return pending.length;
    },
    clearTimer: () => pending.splice(0),
    fire: () => pending.splice(0).forEach((t) => t.fn()),
  };
}

/** A run that can be settled from the test. */
function deferredRuns() {
  const calls: { resolve: () => void; reject: (e: Error) => void }[] = [];
  const run = vi.fn(
    () =>
      new Promise<void>((resolve, reject) => {
        calls.push({ resolve, reject });
      }),
  );
  return { run, calls };
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

describe('createSyncer', () => {
  it('merges requests that arrive while it runs into one more run', async () => {
    const { run, calls } = deferredRuns();
    const syncer = createSyncer({ run, ...timers() });
    const first = syncer.request();
    void syncer.request();
    void syncer.request();
    expect(syncer.syncing).toBe(true);
    expect(run).toHaveBeenCalledTimes(1);
    calls[0]!.resolve();
    await tick();
    expect(run).toHaveBeenCalledTimes(2);
    calls[1]!.resolve();
    await first;
    expect(syncer.syncing).toBe(false);
    expect(run).toHaveBeenCalledTimes(2);
  });

  it('survives a rejected run: no rejection escapes, syncing is false, the retry resumes the sync', async () => {
    const t = timers();
    const { run, calls } = deferredRuns();
    const syncer = createSyncer({ run, ...t });
    const done = syncer.request();
    void syncer.request(); // arrives while the first run is under way
    calls[0]!.reject(new Error('network'));
    await expect(done).resolves.toBeUndefined();
    expect(syncer.syncing).toBe(false);
    expect(t.pending).toHaveLength(1);
    expect(t.pending[0]!.ms).toBe(syncRetryDelay(0));

    t.fire();
    await tick();
    expect(run).toHaveBeenCalledTimes(2);
    calls[1]!.resolve();
    await tick();
    expect(syncer.syncing).toBe(false);
    expect(t.pending).toHaveLength(0);
  });

  it('resumes at once on a new request and drops the waiting retry', async () => {
    const t = timers();
    const run = vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValue(undefined);
    const syncer = createSyncer({ run, ...t });
    await syncer.request();
    expect(t.pending).toHaveLength(1);

    await syncer.request();
    expect(run).toHaveBeenCalledTimes(2);
    expect(t.pending).toHaveLength(0);
  });

  it('backs off between retries and gives up after maxRetries until the next request', async () => {
    const t = timers();
    const onGiveUp = vi.fn();
    const run = vi.fn().mockRejectedValue(new Error('down'));
    const syncer = createSyncer({ run, onGiveUp, maxRetries: 2, ...t });
    await syncer.request();
    expect(t.pending.map((x) => x.ms)).toEqual([syncRetryDelay(0)]);
    t.fire();
    await tick();
    expect(t.pending.map((x) => x.ms)).toEqual([syncRetryDelay(1)]);
    t.fire();
    await tick();
    expect(run).toHaveBeenCalledTimes(3);
    expect(onGiveUp).toHaveBeenCalledTimes(1);
    expect(t.pending).toHaveLength(0);
    expect(syncer.syncing).toBe(false);

    run.mockResolvedValue(undefined);
    await syncer.request();
    expect(run).toHaveBeenCalledTimes(4);
  });
});

describe('syncRetryDelay', () => {
  it('doubles and is capped', () => {
    expect([0, 1, 2].map(syncRetryDelay)).toEqual([1000, 2000, 4000]);
    expect(syncRetryDelay(20)).toBe(10_000);
  });
});
