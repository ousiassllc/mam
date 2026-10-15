import { describe, expect, it, vi } from 'vitest';
import { failOnHttpError, type AjaxFn } from './ajax';
import { createSyncer } from './sync';

describe('failOnHttpError', () => {
  const context = { target: '#x', swap: 'outerHTML' };

  /** An htmx.ajax stand-in: fires htmx:afterRequest on the source with the given status, then resolves (as htmx does for 4xx/5xx). */
  function htmxLike(source: EventTarget, status: number): AjaxFn {
    return async (_verb, url) => {
      source.dispatchEvent(
        new CustomEvent('htmx:afterRequest', { detail: { xhr: { status }, pathInfo: { requestPath: url } } }),
      );
    };
  }

  it('lets a 2xx response through', async () => {
    const source = new EventTarget();
    await expect(failOnHttpError(htmxLike(source, 200), () => source)('get', '/a', context)).resolves.toBeUndefined();
  });

  it('rejects a 5xx response, which makes a Syncer retry', async () => {
    const source = new EventTarget();
    const checked = failOnHttpError(htmxLike(source, 500), () => source);
    await expect(checked('get', '/a', context)).rejects.toThrow('500');
  });

  it('ignores the afterRequest of another URL', async () => {
    const source = new EventTarget();
    const ajax: AjaxFn = async () => {
      source.dispatchEvent(
        new CustomEvent('htmx:afterRequest', { detail: { xhr: { status: 500 }, pathInfo: { requestPath: '/other' } } }),
      );
    };
    await expect(failOnHttpError(ajax, () => source)('get', '/a', context)).resolves.toBeUndefined();
  });

  it('makes a Syncer back off and retry after a 500, then succeed', async () => {
    const source = new EventTarget();
    const statuses = [500, 200];
    const ajax: AjaxFn = (verb, url, ctx) => htmxLike(source, statuses.shift()!)(verb, url, ctx);
    const checked = failOnHttpError(ajax, () => source);
    const timers: (() => void)[] = [];
    const run = vi.fn(() => checked('get', '/messages', context));
    const syncer = createSyncer({
      run,
      setTimer: (fn) => timers.push(fn),
      clearTimer: () => undefined,
      retryDelay: () => 0,
    });
    await syncer.request();
    expect(run).toHaveBeenCalledTimes(1);
    expect(timers).toHaveLength(1);
    timers.shift()!();
    await new Promise((r) => setTimeout(r, 0));
    expect(run).toHaveBeenCalledTimes(2);
    expect(timers).toHaveLength(0);
  });
});
