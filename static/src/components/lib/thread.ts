/** Pure helpers of the chat thread (DOM wiring is in chat.ts). */

export interface Removable {
  id: string;
  remove(): void;
}

/** Event of WebSocket /ws/projects/:id: a hint to refetch, never a body. */
export interface ProjectEvent {
  type: 'message.created' | 'card.updated';
  message_id: string;
}

/**
 * Keeps the first element of every id and removes later ones. A message can arrive
 * twice (the response of the action that created it and the refetch triggered by the
 * WebSocket notification); the thread must show it once.
 */
export function removeDuplicateIds(elements: Iterable<Removable>): number {
  const seen = new Set<string>();
  let removed = 0;
  for (const el of elements) {
    if (!el.id) continue;
    if (seen.has(el.id)) {
      el.remove();
      removed += 1;
    } else {
      seen.add(el.id);
    }
  }
  return removed;
}

/** URL of the request that fetches the messages after `lastMessageId` ('' = the whole thread). */
export function messagesUrl(projectId: string, lastMessageId: string): string {
  const base = `/projects/${encodeURIComponent(projectId)}/messages`;
  return lastMessageId ? `${base}?after=${encodeURIComponent(lastMessageId)}` : base;
}

export function cardUrl(projectId: string, messageId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/messages/${encodeURIComponent(messageId)}`;
}

/** Enter sends, Shift+Enter breaks the line, and Enter that confirms an IME conversion does nothing. */
export function isSendKey(event: { key: string; shiftKey: boolean; isComposing: boolean }): boolean {
  return event.key === 'Enter' && !event.shiftKey && !event.isComposing;
}

/** Whether an error response carries an HTML fragment htmx should swap (a toast or a re-rendered form). */
export function swapsErrorResponse(status: number, contentType: string | null): boolean {
  return status >= 400 && (contentType ?? '').toLowerCase().startsWith('text/html');
}
