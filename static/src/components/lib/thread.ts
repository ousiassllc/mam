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

/** What the client does after the WebSocket came back; the DOM and htmx are behind it (chat.ts). */
export interface CatchUp {
  /** IDs of the cards shown that can still change (the server marks them data-card-pending). */
  pendingMessageIds(): string[];
  /** Replaces the card in place with the server's current one. */
  refetchCard(messageId: string): Promise<unknown>;
  /** Appends the messages newer than the last one shown. */
  syncNew(): Promise<unknown>;
}

/**
 * After a reconnect, events may have been missed: a message.created is covered by fetching what is
 * newer, a card.updated by fetching every card that could still change again (a card that was
 * final when the socket dropped cannot have changed).
 */
export function catchUpAfterReconnect(io: CatchUp): Promise<unknown[]> {
  return Promise.all([io.syncNew(), ...io.pendingMessageIds().map((id) => io.refetchCard(id))]);
}

/** The keyCode of a keydown the IME handled. */
const IME_KEY_CODE = 229;

/**
 * Enter sends, Shift+Enter breaks the line, and Enter that confirms an IME conversion does nothing.
 * Safari/WebKit fires that Enter's keydown after compositionend, so `isComposing` is already false;
 * it is told by `keyCode` 229 (the IME handled the key), which a real Enter never has (it is 13).
 */
export function isSendKey(event: { key: string; shiftKey: boolean; isComposing: boolean; keyCode?: number }): boolean {
  return event.key === 'Enter' && !event.shiftKey && !event.isComposing && event.keyCode !== IME_KEY_CODE;
}

/** Whether an error response carries an HTML fragment htmx should swap (a toast or a re-rendered form). */
export function swapsErrorResponse(status: number, contentType: string | null): boolean {
  return status >= 400 && (contentType ?? '').toLowerCase().startsWith('text/html');
}

/**
 * Whether a finished htmx request succeeded. htmx's own `detail.successful` cannot be used: main.ts clears
 * `isError` for HTML error responses so their toast is swapped, which makes a 4xx/5xx count as successful.
 */
export function isSuccessResponse(xhr: { status: number } | null | undefined): boolean {
  return !!xhr && xhr.status >= 200 && xhr.status < 300;
}

/** What the composer held when a message was sent (files as one comparable key each). */
export interface ComposerSnapshot {
  content: string;
  files: readonly string[];
  replyTo: string;
}

/** Comparable key of a file: two different picks of the same name are told apart by size and date. */
export function fileKey(file: { name: string; size: number; lastModified: number }): string {
  return `${file.name}:${file.size}:${file.lastModified}`;
}

/**
 * Which composer fields to clear once a send has succeeded. The input stays editable while the
 * request runs, so only a field still holding what was sent is cleared; anything typed, attached or
 * replied to in the meantime is the next message and is kept.
 */
export function fieldsToClear(
  sent: ComposerSnapshot,
  current: ComposerSnapshot,
): { content: boolean; files: boolean; replyTo: boolean } {
  return {
    content: current.content === sent.content,
    files: current.files.length === sent.files.length && current.files.every((key, i) => key === sent.files[i]),
    replyTo: current.replyTo === sent.replyTo,
  };
}
