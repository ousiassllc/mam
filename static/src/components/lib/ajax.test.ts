import { describe, expect, it, vi } from 'vitest';
import { AJAX_FAILED_MESSAGE, createAjaxGet } from './ajax';

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
