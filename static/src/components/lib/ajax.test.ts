import { describe, expect, it, vi } from 'vitest';
import { AJAX_FAILED_MESSAGE, createAjaxGet, failOnHttpError, type AjaxFn } from './ajax';
import { createSyncer } from './sync';

describe('createAjaxGet', () => {
  const context = { target: '#x', swap: 'outerHTML' };

  it('reports success without a message', async () => {
    const ajax = vi.fn().mockResolvedValue(undefined);
    const notify = vi.fn();
    await expect(createAjaxGet(ajax, notify)('/a', context)).resolves.toBe(true);
    expect(ajax).toHaveBeenCalledWith('get', '/a', { ...context, source: '#x' });
    expect(notify).not.toHaveBeenCalled();
  });

  it('turns a rejected request into a message and false instead of an unhandled rejection', async () => {
    const ajax = vi.fn().mockRejectedValue(new Error('target not found'));
    const notify = vi.fn();
    await expect(createAjaxGet(ajax, notify)('/a', context)).resolves.toBe(false);
    expect(notify).toHaveBeenCalledWith(AJAX_FAILED_MESSAGE);
  });

  it('issues every request from its own target so concurrent refetches do not share <body>', async () => {
    const ajax = vi.fn().mockResolvedValue(undefined);
    const get = createAjaxGet(ajax, vi.fn());
    await get('/c1', { target: '#msg-1', swap: 'outerHTML' });
    await get('/c2', { target: '#msg-2', swap: 'outerHTML' });
    expect(ajax.mock.calls.map(([, , c]) => c.source)).toEqual(['#msg-1', '#msg-2']);
  });

  it('keeps an explicit source', async () => {
    const ajax = vi.fn().mockResolvedValue(undefined);
    await createAjaxGet(ajax, vi.fn())('/a', { ...context, source: '#y' });
    expect(ajax.mock.calls[0]?.[2].source).toBe('#y');
  });
});

describe('failOnHttpError', () => {
  const context = { target: '#x', swap: 'outerHTML' };

  /** An htmx.ajax stand-in: fires htmx:afterRequest on the source with the given status, then resolves (as htmx does for 4xx/5xx). */
  function htmxLike(source: EventTarget, status: number): AjaxFn {
    return async (_verb, url) => {
      source.dispatchEvent(new CustomEvent('htmx:afterRequest', { detail: { xhr: { status }, pathInfo: { requestPath: url } } }));
    };
  }

  it('lets a 2xx response through', async () => {
    const source = new EventTarget();
    await expect(failOnHttpError(htmxLike(source, 200), () => source)('get', '/a', context)).resolves.toBeUndefined();
  });

  it('rejects a 5xx response, which makes ajaxGet report false', async () => {
    const source = new EventTarget();
    const checked = failOnHttpError(htmxLike(source, 500), () => source);
    await expect(checked('get', '/a', context)).rejects.toThrow('500');
    const notify = vi.fn();
    await expect(createAjaxGet(checked, notify)('/a', context)).resolves.toBe(false);
    expect(notify).toHaveBeenCalledWith(AJAX_FAILED_MESSAGE);
  });

  it('ignores the afterRequest of another URL', async () => {
    const source = new EventTarget();
    const ajax: AjaxFn = async () => {
      source.dispatchEvent(new CustomEvent('htmx:afterRequest', { detail: { xhr: { status: 500 }, pathInfo: { requestPath: '/other' } } }));
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
    const syncer = createSyncer({ run, setTimer: (fn) => timers.push(fn), clearTimer: () => undefined, retryDelay: () => 0 });
    await syncer.request();
    expect(run).toHaveBeenCalledTimes(1);
    expect(timers).toHaveLength(1);
    timers.shift()!();
    await new Promise((r) => setTimeout(r, 0));
    expect(run).toHaveBeenCalledTimes(2);
    expect(timers).toHaveLength(0);
  });
});
