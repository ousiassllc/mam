/**
 * The questions of the model manager that are asked in the page itself: the age confirmation of adult models and the
 * license of a model (organisms/modelmgr: a `<section role="dialog" data-inline-dialog>` that a request swaps into
 * `#mm-dialog`). They are not the modal of the settings screen (`components/modal`), so what that gives is done here, small:
 *
 * - When the dialog arrives the focus moves into it: to the reason of a refusal when there is one, else to the dialog
 *   itself (so that a reader starts at its name and Tab reaches its first control). An answer that draws the dialog again
 *   (a refusal) does the same; any other swap of the page leaves the focus alone.
 * - Esc closes it, through its own `data-inline-dialog-close` button, so that the server's answer is what closes it.
 * - When it is gone, the focus goes back to the control that opened it or, when the swap replaced that control (the card
 *   of the setting, the row of the model), to the first control of the place named by `data-return-focus` (a selector).
 * - The focus never scrolls the page by itself (`preventScroll`): the dialog stands above the list, so what brings it into
 *   view is one explicit, minimal scroll when it opens, and when it is gone the page is put back where it was when the
 *   control was pressed. Answering the question (or closing it) leaves the reader where they were, not at the card the
 *   focus went back to, which can be screens away from the dialog (#908).
 *
 * Install it before `installFocusRestore`: the focus that is back on a control is not touched by that one.
 */

const DIALOG = '[data-inline-dialog]';
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

  root.addEventListener('htmx:afterSwap', (event) => {
    const dlg = document.querySelector<HTMLElement>(DIALOG);
    if (dlg) {
      if (dlg === shown) return;
      const requester = (event as CustomEvent<SwapDetail>).detail?.requestConfig?.elt ?? null;
      // A request from inside the dialog redraws it (a refusal): the control that opened it stays the one to go back to.
      if (!shown || (requester && !shown.contains(requester))) {
        opener = requester;
        origin = (requester && pressedAt.get(requester)) || null;
      }
      const opening = shown === null;
      shown = dlg;
      returnTo = dlg.getAttribute('data-return-focus');
      if (opening) dlg.scrollIntoView({ block: 'nearest', inline: 'nearest' });
      focusOn(dlg.querySelector<HTMLElement>('[role="alert"]') ?? dlg);
      return;
    }
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
