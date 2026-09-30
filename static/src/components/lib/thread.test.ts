import { describe, expect, it } from 'vitest';
import { cardUrl, isSendKey, messagesUrl, removeDuplicateIds, swapsErrorResponse } from './thread';

function el(id: string) {
  return { id, removed: false, remove() { this.removed = true; } };
}

describe('removeDuplicateIds', () => {
  it('keeps the first element of each id', () => {
    const items = [el('msg-1'), el('msg-2'), el('msg-1'), el('msg-2'), el('msg-3')];
    expect(removeDuplicateIds(items)).toBe(2);
    expect(items.map((i) => i.removed)).toEqual([false, false, true, true, false]);
  });
});

describe('URLs', () => {
  it('asks for the whole thread or only what is newer', () => {
    expect(messagesUrl('p1', '')).toBe('/projects/p1/messages');
    expect(messagesUrl('p1', 'm 1')).toBe('/projects/p1/messages?after=m%201');
    expect(cardUrl('p1', 'm1')).toBe('/projects/p1/messages/m1');
  });
});

describe('isSendKey', () => {
  it('sends on plain Enter only', () => {
    expect(isSendKey({ key: 'Enter', shiftKey: false, isComposing: false })).toBe(true);
    expect(isSendKey({ key: 'Enter', shiftKey: true, isComposing: false })).toBe(false);
    expect(isSendKey({ key: 'Enter', shiftKey: false, isComposing: true })).toBe(false);
    expect(isSendKey({ key: 'a', shiftKey: false, isComposing: false })).toBe(false);
  });
});

describe('swapsErrorResponse', () => {
  it('swaps only HTML error responses', () => {
    expect(swapsErrorResponse(422, 'text/html; charset=utf-8')).toBe(true);
    expect(swapsErrorResponse(403, 'application/json')).toBe(false);
    expect(swapsErrorResponse(200, 'text/html')).toBe(false);
    expect(swapsErrorResponse(500, null)).toBe(false);
  });
});
