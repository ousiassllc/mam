/**
 * The questions of the model manager: the age confirmation of adult models and the license of a model (organisms/modelmgr: a
 * `<section role="dialog" data-inline-dialog>` that a request swaps into `#mm-dialog`, a native `<dialog data-inline-dialog-host>`,
 * molecules/modal InlineHost). A question is a centred modal with a scrim like every confirmation (the UI guide, O2): when one arrives the host
 * is opened with showModal() (the page behind it is inert, the focus stays inside) and when the answer takes it away the host is closed. Esc, a click
 * on the scrim (static/src/components/scrim) and the question's own close button close it; closing empties it. It is not the modal of the
 * settings screen (`components/modal`), so what that gives is done here, small:
 *
 * - When the dialog arrives the focus moves into it: to the reason of a refusal when there is one, else to the dialog
 *   itself (so that a reader starts at its name and Tab reaches its first control). An answer that draws the dialog again
 *   (a refusal) does the same; any other swap of the page leaves the focus alone.
 * - Esc closes it, through its own `data-inline-dialog-close` button, so that the server's answer is what closes it.
 * - When it is gone, the focus goes back to the control that opened it or, when the swap replaced that control (the card
 *   of the setting, the row of the model), to the first control of the place named by `data-return-focus` (a selector).
 * - The focus never scrolls the page by itself (`preventScroll`): the dialog stands above the list, so what brings it into
 *   view is one explicit, minimal scroll when it opens (also when another control swaps a different question in while one
 *   is open; a redraw from inside the dialog does not scroll), and when it is gone the page is put back where it was when the
 *   control was pressed. Answering the question (or closing it) leaves the reader where they were, not at the card the
 *   focus went back to, which can be screens away from the dialog (#908).
 *
 * Install it before `installFocusRestore`: the focus that is back on a control is not touched by that one.
 */

const DIALOG = '[data-inline-dialog]';
const HOST = 'dialog[data-inline-dialog-host]';
const CLOSE = '[data-inline-dialog-close]';
const CONTROL = 'a[href],button:not(:disabled),input:not([type="hidden"]):not(:disabled),select,textarea';

interface SwapDetail {
  requestConfig?: { elt?: Element };
}

interface RequestDetail {
  elt?: Element;
}

interface Scroll {
  left: number;
  top: number;
}

function focusOn(el: HTMLElement | null | undefined): boolean {
  if (!el?.isConnected) return false;
  el.focus({ preventScroll: true });
  return document.activeElement === el;
}

/** Installs the behaviour of the inline dialogs. Call it once (main.ts). */
export function installInlineDialog(root: Document = document): void {
  /** The dialog that was given the focus: another one on the page is a new question. */
  let shown: HTMLElement | null = null;
  let opener: Element | null = null;
  let returnTo: string | null = null;
  /** Where the page was when each control sent its request: the place to go back to when the dialog it opened is gone. */
  const pressedAt = new WeakMap<Element, Scroll>();
  let origin: Scroll | null = null;

  root.addEventListener('htmx:beforeRequest', (event) => {
    const elt = (event as CustomEvent<RequestDetail>).detail?.elt;
    if (elt) pressedAt.set(elt, { left: window.scrollX, top: window.scrollY });
  });

  /** The question is gone: the page goes back to where it was, and the focus to the control that asked (or to the place of its card). */
  function gone(): void {
    if (!shown) return;
    shown = null;
    const from = opener;
    opener = null;
    if (origin) window.scrollTo({ left: origin.left, top: origin.top, behavior: 'instant' });
    origin = null;
    // The focus the user (or htmx) put somewhere else is not ours; the dialog's own control that had it is gone.
    if (document.activeElement && document.activeElement !== document.body) return;
    if (focusOn(from as HTMLElement | null)) return;
    const place = returnTo ? document.querySelector(returnTo) : null;
    focusOn(place?.querySelector<HTMLElement>(CONTROL));
  }

  /** Opens the host while it holds a question and closes it when it does not (closing is a swap that took the question away). */
  function syncHost(): void {
    const host = document.querySelector<HTMLDialogElement>(HOST);
    if (!host) return;
    const holdsQuestion = host.querySelector(DIALOG) !== null;
    if (holdsQuestion && !host.open) host.showModal();
    else if (!holdsQuestion && host.open) host.close();
  }

  // A host closed by Esc or by the scrim goes with its question: what it held is gone, so that it does not open again with a swap.
  root.addEventListener(
    'close',
    (event) => {
      const host = event.target as Element | null;
      if (!host?.matches(HOST)) return;
      host.replaceChildren();
      gone();
    },
    true,
  );

  root.addEventListener('htmx:afterSwap', (event) => {
    syncHost();
    const dlg = document.querySelector<HTMLElement>(DIALOG);
    if (dlg) {
      if (dlg === shown) return;
      const requester = (event as CustomEvent<SwapDetail>).detail?.requestConfig?.elt ?? null;
      // A request from inside the dialog redraws it (a refusal): the control that opened it stays the one to go back to.
      // Any other control that draws a dialog (the first one, or a different question while one is open) is a new question:
      // the dialog is a modal over the page, so nothing is scrolled to it, and when it is gone the page is back where that
      // control was.
      const isNewQuestion = !shown || (!!requester && !shown.contains(requester));
      if (isNewQuestion) {
        opener = requester;
        origin = (requester && pressedAt.get(requester)) || null;
      }
      shown = dlg;
      returnTo = dlg.getAttribute('data-return-focus');
      focusOn(dlg.querySelector<HTMLElement>('[role="alert"]') ?? dlg);
      return;
    }
    gone();
  });

  root.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || event.defaultPrevented || !shown?.isConnected) return;
    const target = event.target as Element | null;
    if (target && target !== document.body && !shown.contains(target)) return;
    const close = shown.querySelector<HTMLElement>(CLOSE);
    if (!close) return;
    event.preventDefault();
    close.click();
  });
}
