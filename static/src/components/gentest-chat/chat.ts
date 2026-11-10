/**
 * The chat of the single generation screen (#1138; the Templ `organisms/gentest/chat`).
 *
 * The thread and the cards come from the server (htmx appends what a message answers to `#gtc-thread`); this does the four things a browser has to do around that:
 * Enter sends the message and Shift+Enter breaks the line (an Enter that confirms an IME conversion does nothing), the box is emptied once the message was
 * taken (a refused message keeps its words), the thread follows a new entry (a swap that only redraws one, the result block asking for itself every 2 seconds, follows only while the reader is at the bottom), and a question that has been answered is turned off: only the option cards
 * of the newest message of the assistant can be tapped, so an old card cannot send a stale answer.
 *
 * A generation never starts here: the only thing that does is the button of a confirmation card, a plain form of the server's.
 */
import { type ComposerSnapshot, fieldsToClear, isSendKey, isSuccessResponse } from '../lib/thread';

const THREAD = '#gtc-thread';
const COMPOSER = '#gtc-composer';
const CONTENT = '#gtc-content';
/** How far (px) from the bottom still counts as "at the bottom": a line of the last entry may be cut off. */
const NEAR_BOTTOM = 80;

/** Turns off every group of option cards but the last one (the question that is still open). */
export function retireAnswered(thread: ParentNode): void {
  const groups = [...thread.querySelectorAll<HTMLElement>('[data-gtc-options]')];
  for (const group of groups.slice(0, -1)) {
    group.dataset.answered = '';
    group.inert = true;
  }
}

function follow(thread: HTMLElement): void {
  thread.scrollTop = thread.scrollHeight;
}

function atBottom(thread: HTMLElement): boolean {
  return thread.scrollHeight - thread.scrollTop - thread.clientHeight <= NEAR_BOTTOM;
}

/** What the composer holds: the chat has no files and no reply target, so only the words. */
function snapshot(box: HTMLTextAreaElement): ComposerSnapshot {
  return { content: box.value, files: [], replyTo: '' };
}

const installed = new WeakSet<Document>();

/** Installs the chat. Call it once (main.ts): it works on whatever page has a `#gtc-thread`. */
export function installGentestChat(doc: Document = document): void {
  if (installed.has(doc)) return;
  installed.add(doc);

  doc.addEventListener('keydown', (event) => {
    const box = (event.target as Element | null)?.closest<HTMLTextAreaElement>(CONTENT);
    if (!box || !isSendKey(event)) return;
    event.preventDefault();
    if (box.value.trim() !== '') box.form?.requestSubmit();
  });

  // The box stays editable while the message is on its way: only the words that were sent are cleared, what was typed meanwhile is the next message
  // (as the Project chat's fieldsToClear does). detail.successful is not used: main.ts swaps the HTML of a 4xx/5xx answer (the alert), which makes htmx
  // report it as successful too.
  let sent: ComposerSnapshot | undefined;
  doc.addEventListener('htmx:beforeRequest', (event) => {
    const elt = (event as CustomEvent<{ elt: Element }>).detail.elt;
    if (!elt.matches(COMPOSER)) return;
    const box = elt.querySelector<HTMLTextAreaElement>(CONTENT);
    if (box) sent = snapshot(box);
  });

  doc.addEventListener('htmx:afterRequest', (event) => {
    const { elt, xhr } = (event as CustomEvent<{ elt: Element; xhr?: { status: number } }>).detail;
    if (!elt.matches(COMPOSER)) return;
    const was = sent;
    sent = undefined;
    if (!was || !isSuccessResponse(xhr)) return;
    const box = elt.querySelector<HTMLTextAreaElement>(CONTENT);
    if (box && fieldsToClear(was, snapshot(box)).content) {
      box.value = '';
      box.focus();
    }
  });

  // Whether the reader is at the bottom is read before a swap grows the thread, and what is new is told by the number of entries: a swap that replaces an
  // entry in place (the result block polling itself) adds none.
  let pinned = true;
  const entries = new WeakMap<HTMLElement, number>();
  doc.addEventListener('htmx:beforeSwap', (event) => {
    const thread = doc.querySelector<HTMLElement>(THREAD);
    if (thread && (event.target as Element | null)?.closest(THREAD)) pinned = atBottom(thread);
  });

  doc.addEventListener('htmx:afterSwap', (event) => {
    const thread = doc.querySelector<HTMLElement>(THREAD);
    if (!thread || !(event.target as Element | null)?.closest(THREAD)) return;
    retireAnswered(thread);
    const added = thread.childElementCount > (entries.get(thread) ?? 0);
    entries.set(thread, thread.childElementCount);
    if (added || pinned) follow(thread);
  });
}
