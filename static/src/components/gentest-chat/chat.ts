/**
 * The chat of the single generation screen (#1138; the Templ `organisms/gentest/chat`).
 *
 * The thread and the cards come from the server (htmx appends what a message answers to `#gtc-thread`); this does the four things a browser has to do around that:
 * Enter sends the message and Shift+Enter breaks the line (an Enter that confirms an IME conversion does nothing), the box is emptied once the message was
 * taken (a refused message keeps its words), the thread follows the newest entry, and a question that has been answered is turned off: only the option cards
 * of the newest message of the assistant can be tapped, so an old card cannot send a stale answer.
 *
 * A generation never starts here: the only thing that does is the button of a confirmation card, a plain form of the server's.
 */
import { isSendKey, isSuccessResponse } from '../lib/thread';

const THREAD = '#gtc-thread';
const COMPOSER = '#gtc-composer';
const CONTENT = '#gtc-content';

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

  doc.addEventListener('htmx:afterRequest', (event) => {
    // detail.successful is not used: main.ts swaps the HTML of a 4xx/5xx answer (the alert), which makes htmx report it as successful too.
    const { elt, xhr } = (event as CustomEvent<{ elt: Element; xhr?: { status: number } }>).detail;
    if (!isSuccessResponse(xhr) || !elt.matches(COMPOSER)) return;
    const box = elt.querySelector<HTMLTextAreaElement>(CONTENT);
    if (box) {
      box.value = '';
      box.focus();
    }
  });

  doc.addEventListener('htmx:afterSwap', (event) => {
    const thread = doc.querySelector<HTMLElement>(THREAD);
    if (!thread || !(event.target as Element | null)?.closest(THREAD)) return;
    retireAnswered(thread);
    follow(thread);
  });
}
