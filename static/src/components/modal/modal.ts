import { requestFailureMessage, type RequestFailure } from '../lib/errors';

/**
 * The modal dialog of the settings screen (spec/components/shared-ui-parts.md §4): one native <dialog id="mam-modal"
 * data-modal> per page, whose body an HTMX request fills.
 *
 * - A button with `data-modal-open` (molecules/modal OpenAttrs) asks for the body; when it arrives the dialog is
 *   opened with showModal(), which keeps the focus inside it and makes the rest of the page inert.
 * - It is closed by Esc (the native `cancel`), by a `data-modal-close` button and by the `modal-close` event that a
 *   saved form's answer sets off (HX-Trigger-After-Swap). A click on the backdrop does not close it.
 * - When it is closed the body is emptied (a token that was shown, what was typed, must not stay in the DOM) and the
 *   focus goes back to the button that opened it, or to the control that took its place when its row was replaced.
 * - A request from inside it that gets no HTML answer (server down, timeout) says so in `#mam-modal-failure`: the toast
 *   region lies behind the dialog, which is in the top layer.
 */

const DIALOG = 'dialog[data-modal]';
const BODY = '#mam-modal-body';
const FAILURE = '#mam-modal-failure';
const REASON = 'mam-modal-error';
const TITLE = 'mam-modal-title';
const DESCRIPTION = 'mam-modal-desc';

/** Where the control that opened the modal was. */
interface Opener {
  el: Element;
  focusKey: string | null;
  /** The id of the nearest ancestor with one: the row that a save replaces. */
  rowId: string | null;
}

interface RequestDetail {
  xhr?: XMLHttpRequest;
  requestConfig?: { elt?: Element; verb?: string };
}

const detailOf = (event: Event) => (event as CustomEvent<RequestDetail>).detail;
const targetOf = (event: Event) => event.target as Element | null;

function dialog(): HTMLDialogElement | null {
  return document.querySelector<HTMLDialogElement>(DIALOG);
}

/** Whether `el` is inside the modal dialog: a failure there is shown by the dialog, not by a toast. */
export function insideModal(el: Element | null | undefined): boolean {
  return !!el?.closest?.(DIALOG);
}

function focusable(el: HTMLElement | null | undefined): boolean {
  if (!el?.isConnected) return false;
  el.focus({ preventScroll: true });
  return document.activeElement === el;
}

/** The control to start with: the one marked `autofocus`, else the reason of a refusal, else the first control. */
function initialFocus(dlg: HTMLDialogElement): HTMLElement | null {
  const body = dlg.querySelector(BODY);
  return (
    body?.querySelector<HTMLElement>('[autofocus]') ??
    body?.querySelector<HTMLElement>('#mam-modal-error') ??
    body?.querySelector<HTMLElement>('a[href],button:not(:disabled),input:not([type="hidden"]),select,textarea') ??
    dlg.querySelector<HTMLElement>('[data-modal-close]')
  );
}

/** Names the dialog by its heading and, when the body has one, describes it by its first paragraph. */
function label(dlg: HTMLDialogElement): void {
  if (document.getElementById(DESCRIPTION)) dlg.setAttribute('aria-describedby', DESCRIPTION);
  else dlg.removeAttribute('aria-describedby');
  if (document.getElementById(TITLE)) dlg.setAttribute('aria-labelledby', TITLE);
}

/**
 * A field that said its refusal was about it (`aria-invalid`, `aria-describedby` = the reason) must not point at a reason
 * that an answer has taken away (the draft connection test deletes `#mam-modal-error` out of band).
 */
function dropGoneReason(dlg: HTMLDialogElement): void {
  if (document.getElementById(REASON)) return;
  for (const field of dlg.querySelectorAll(`[aria-describedby~="${REASON}"]`)) {
    const rest = (field.getAttribute('aria-describedby') ?? '').split(/\s+/).filter((id) => id && id !== REASON);
    if (rest.length > 0) field.setAttribute('aria-describedby', rest.join(' '));
    else field.removeAttribute('aria-describedby');
    field.removeAttribute('aria-invalid');
  }
}

function hideFailure(dlg: HTMLDialogElement): void {
  const failure = dlg.querySelector<HTMLElement>(FAILURE);
  if (!failure) return;
  failure.hidden = true;
  failure.textContent = '';
}

/** The control of the replaced row with the same key and kind as `opener`, else the row itself. */
function restoreFocus(opener: Opener): void {
  if (focusable(opener.el as HTMLElement)) return;
  const row = opener.rowId ? document.getElementById(opener.rowId) : null;
  if (row) {
    const same = [...row.querySelectorAll<HTMLElement>('[data-focus-key]')].find(
      (el) =>
        el.localName === opener.el.localName &&
        el.getAttribute('data-focus-key') === opener.focusKey &&
        !el.matches(':disabled'),
    );
    if (focusable(same)) return;
    if (!row.hasAttribute('tabindex')) row.setAttribute('tabindex', '-1');
    if (focusable(row)) return;
  }
  // The list is gone too: the heading of the category, so that a reader starts there.
  const heading = document.getElementById('settings-category-title');
  if (heading) {
    if (!heading.hasAttribute('tabindex')) heading.setAttribute('tabindex', '-1');
    focusable(heading);
  }
}

/** Installs the modal. Call it once (main.ts). */
export function installModal(root: Document = document): void {
  let opener: Opener | null = null;
  // The GET requests sent from inside the dialog that have not been answered (a poll, a `hx-trigger="load"`): closing the
  // dialog abandons them. A POST / PUT / DELETE is not: the server may have applied it, and its answer (the out-of-band
  // rows, a one-time token) must arrive; the afterSwap guard below empties the closed dialog's body again.
  const inFlight = new Set<XMLHttpRequest>();

  root.addEventListener('htmx:beforeRequest', (event) => {
    const elt = targetOf(event);
    if (!elt) return;
    if (elt.hasAttribute('data-modal-open')) {
      opener = {
        el: elt,
        focusKey: elt.getAttribute('data-focus-key'),
        rowId: elt.parentElement?.closest('[id]')?.id ?? null,
      };
    }
    const dlg = dialog();
    if (dlg && insideModal(elt)) hideFailure(dlg);
  });

  root.addEventListener('htmx:beforeSend', (event) => {
    const { xhr, requestConfig } = detailOf(event);
    if (!xhr || requestConfig?.verb !== 'get' || !insideModal(targetOf(event))) return;
    inFlight.add(xhr);
    xhr.addEventListener('loadend', () => inFlight.delete(xhr));
  });

  // The answer of the request that opened the modal (or of a form inside it, which fills the body again): open the
  // dialog when it is not open. The new content is not processed by htmx until the swap settles (the hx-* attributes of
  // its form), and a form that is sent before that is sent by the browser itself, with the key in the address. So the
  // body is inert until then, and the control to start with takes the focus when it settles.
  root.addEventListener('htmx:afterSwap', (event) => {
    // The event is triggered on each element the swap changed, and its detail is shared: take the body's own.
    const body = targetOf(event);
    if (!body?.matches(BODY)) return;
    const requester = detailOf(event).requestConfig?.elt;
    const dlg = dialog();
    if (!dlg) return;
    // Nothing asked for the modal. An answer that comes after it was closed (a form's, which may hold what is shown
    // once, like a token) is not kept in the closed dialog.
    if (!dlg.open && !requester?.hasAttribute('data-modal-open')) {
      (body as HTMLElement).replaceChildren();
      return;
    }
    label(dlg);
    if (!dlg.open) dlg.showModal();
    (body as HTMLElement).inert = true;
  });

  // Any answer that lands in the dialog may take the reason away: the fields stop pointing at it in the same swap.
  root.addEventListener('htmx:afterSwap', (event) => {
    const dlg = dialog();
    if (dlg && insideModal(targetOf(event))) dropGoneReason(dlg);
  });

  root.addEventListener('htmx:afterSettle', (event) => {
    const body = targetOf(event);
    const dlg = dialog();
    if (!dlg?.open || !body?.matches(BODY)) return;
    (body as HTMLElement).inert = false;
    focusable(initialFocus(dlg));
  });

  root.addEventListener('click', (event) => {
    const button = (event.target as Element | null)?.closest?.('[data-modal-close]');
    const dlg = dialog();
    if (button && dlg?.contains(button)) dlg.close();
  });

  // A saved form's answer asks for the close (HX-Trigger-After-Swap: modal-close).
  root.addEventListener('modal-close', () => {
    const dlg = dialog();
    if (dlg?.open) dlg.close();
  });

  // A page that goes into the history is not kept with the modal open: it would come back as a plain dialog without
  // the page behind it made inert, and with what it showed (a token, what was typed). The event comes before the
  // snapshot is taken, and `close` only tells later, so the body is emptied here.
  root.addEventListener('htmx:beforeHistorySave', () => {
    const dlg = dialog();
    if (!dlg) return;
    if (dlg.open) dlg.close();
    dlg.querySelector(BODY)?.replaceChildren();
    hideFailure(dlg);
  });

  // `close` does not bubble: listen in the capture phase.
  root.addEventListener(
    'close',
    (event) => {
      const dlg = dialog();
      if (!dlg || event.target !== dlg) return;
      const body = dlg.querySelector<HTMLElement>(BODY);
      if (body) body.inert = false;
      body?.replaceChildren();
      // A GET that is on its way must not come back into the emptied body (one that has arrived is DONE: its handlers
      // are still running, and aborting it would clear its status). A POST / PUT / DELETE is let to arrive: the afterSwap
      // guard empties the body again after the out-of-band parts (the rows) are swapped.
      for (const xhr of inFlight) if (xhr.readyState !== XMLHttpRequest.DONE) xhr.abort();
      inFlight.clear();
      hideFailure(dlg);
      dlg.removeAttribute('aria-describedby');
      const from = opener;
      opener = null;
      if (from) restoreFocus(from);
    },
    true,
  );

  for (const type of [
    'htmx:responseError',
    'htmx:sendError',
    'htmx:timeout',
  ] as const satisfies readonly RequestFailure[]) {
    root.addEventListener(type, (event) => {
      const dlg = dialog();
      const failure = dlg?.querySelector<HTMLElement>(FAILURE);
      if (!dlg?.open || !failure || !insideModal(targetOf(event))) return;
      failure.textContent = requestFailureMessage(type, detailOf(event).xhr?.status);
      failure.hidden = false;
    });
  }
}
