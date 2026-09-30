import { describe, expect, it, vi } from 'vitest';
import { AJAX_FAILED_MESSAGE, createAjaxGet } from './ajax';

describe('createAjaxGet', () => {
  const context = { target: '#x', swap: 'outerHTML' };

  it('reports success without a message', async () => {
    const ajax = vi.fn().mockResolvedValue(undefined);
    const notify = vi.fn();
    await expect(createAjaxGet(ajax, notify)('/a', context)).resolves.toBe(true);
    expect(ajax).toHaveBeenCalledWith('get', '/a', context);
    expect(notify).not.toHaveBeenCalled();
  });

  it('turns a rejected request into a message and false instead of an unhandled rejection', async () => {
    const ajax = vi.fn().mockRejectedValue(new Error('target not found'));
    const notify = vi.fn();
    await expect(createAjaxGet(ajax, notify)('/a', context)).resolves.toBe(false);
    expect(notify).toHaveBeenCalledWith(AJAX_FAILED_MESSAGE);
  });
});
