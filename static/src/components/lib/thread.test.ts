import { describe, expect, it } from 'vitest';
import { catchUpAfterReconnect, cardUrl, fieldsToClear, fileKey, isSendKey, messagesUrl, isSuccessResponse, removeDuplicateIds, swapsErrorResponse } from './thread';

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
    expect(isSendKey({ key: 'Enter', shiftKey: false, isComposing: false, keyCode: 13 })).toBe(true);
  });

  it('ignores the Enter that confirms an IME conversion on Safari (keydown after compositionend: isComposing false, keyCode 229)', () => {
    expect(isSendKey({ key: 'Enter', shiftKey: false, isComposing: false, keyCode: 229 })).toBe(false);
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

describe('isSuccessResponse', () => {
  it('is true for 2xx only', () => {
    expect(isSuccessResponse({ status: 200 })).toBe(true);
    expect(isSuccessResponse({ status: 204 })).toBe(true);
    for (const status of [0, 302, 400, 403, 409, 422, 429, 500, 502]) expect(isSuccessResponse({ status })).toBe(false);
    expect(isSuccessResponse(undefined)).toBe(false);
  });
});

describe('fieldsToClear', () => {
  const sent = { content: '最初のメッセージ', files: ['a.png:10:1'], replyTo: 'm1' };

  it('clears everything that is still what was sent', () => {
    expect(fieldsToClear(sent, { ...sent })).toEqual({ content: true, files: true, replyTo: true });
  });

  it('keeps the next message typed while the request was running', () => {
    const typed = { ...sent, content: '最初のメッセージ。続き' };
    expect(fieldsToClear(sent, typed)).toEqual({ content: false, files: true, replyTo: true });
    expect(fieldsToClear(sent, { ...sent, content: '' }).content).toBe(false);
  });

  it('keeps attachments and the reply target changed meanwhile', () => {
    expect(fieldsToClear(sent, { ...sent, files: ['a.png:10:1', 'b.png:20:2'] }).files).toBe(false);
    expect(fieldsToClear(sent, { ...sent, files: [] }).files).toBe(false);
    expect(fieldsToClear(sent, { ...sent, replyTo: 'm2' }).replyTo).toBe(false);
    expect(fieldsToClear({ content: 'x', files: [], replyTo: '' }, { content: 'x', files: [], replyTo: '' }).files).toBe(true);
  });
});

describe('fileKey', () => {
  it('tells two picks of the same name apart', () => {
    expect(fileKey({ name: 'a.png', size: 1, lastModified: 2 })).not.toBe(fileKey({ name: 'a.png', size: 1, lastModified: 3 }));
  });
});
