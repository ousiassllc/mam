import { describe, expect, it } from 'vitest';
import { catchUpAfterReconnect, cardUrl, isSendKey, messagesUrl, removeDuplicateIds, swapsErrorResponse } from './thread';

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

describe('catchUpAfterReconnect', () => {
  it('fetches what is new and every card that could still change, and nothing else', async () => {
    const refetched: string[] = [];
    let synced = 0;
    await catchUpAfterReconnect({
      pendingMessageIds: () => ['approval-1', 'progress-2'],
      refetchCard: async (id) => void refetched.push(id),
      syncNew: async () => void (synced += 1),
    });
    expect(synced).toBe(1);
    expect(refetched).toEqual(['approval-1', 'progress-2']);
  });

  it('only fetches what is new when no card is pending', async () => {
    const refetchCard = async () => {
      throw new Error('no card to fetch');
    };
    let synced = 0;
    await catchUpAfterReconnect({ pendingMessageIds: () => [], refetchCard, syncNew: async () => void (synced += 1) });
    expect(synced).toBe(1);
  });
});
