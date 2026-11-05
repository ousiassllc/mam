/**
 * The confirmation modal (the Ousiass UI guide, O2): what is deleted, removed or cannot be taken back is asked in a centred modal with a scrim that
 * closes it, not in the browser's own confirm() box. The markup is defined once, in molecules/modal (modal.Confirm); the page shell embeds it as
 * #confirm-template and this module clones it, fills the question in and opens it with showModal() (the page behind is inert, the focus stays inside).
 *
 * - `confirmDialog(question)` asks and answers `true` (the button that goes on) or `false` (キャンセル, Esc, a click on the scrim).
 * - An element with `hx-confirm` asks the same way before its request goes out (htmx's own confirm() is taken over through `htmx:confirm`).
 */
import { createLogger } from '../lib/logger';

const log = createLogger('confirm');
const TEMPLATE_ID = 'confirm-template';

/** Asks `question` in the confirmation modal; resolves `true` when the user goes on. A page without #confirm-template cannot ask, so it does not go on. */
export function confirmDialog(question: string): Promise<boolean> {
  const template = document.getElementById(TEMPLATE_ID);
  const dialog = template instanceof HTMLTemplateElement ? template.content.firstElementChild?.cloneNode(true) : null;
  if (!(dialog instanceof HTMLDialogElement)) {
    log.warn(`#${TEMPLATE_ID} is missing; cannot ask`, question);
    return Promise.resolve(false);
  }
  const message = dialog.querySelector('[data-confirm-message]');
  if (message) message.textContent = question;
  const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  return new Promise((resolve) => {
    let answer = false;
    dialog.addEventListener('click', (event) => {
      const button = event.target instanceof Element ? event.target.closest('button') : null;
      if (!button) return;
      if (button.hasAttribute('data-confirm-ok')) answer = true;
      if (answer || button.hasAttribute('data-confirm-cancel')) dialog.close();
    });
    dialog.addEventListener('close', () => {
      dialog.remove();
      // The control that asked gets the focus back, when the swap that follows has not replaced it.
      if (opener?.isConnected) opener.focus({ preventScroll: true });
      resolve(answer);
    });
    document.body.append(dialog);
    dialog.showModal();
  });
}

/** Takes over the confirmation of every `hx-confirm` element: the question is asked in the modal, and the request goes out when it is answered yes. */
export function installConfirm(root: Document = document): void {
  root.addEventListener('htmx:confirm', (event) => {
    const detail = (event as CustomEvent<{ question?: string | null; issueRequest: (skip: boolean) => void }>).detail;
    if (!detail.question) return;
    event.preventDefault();
    void confirmDialog(detail.question).then((yes) => {
      if (yes) detail.issueRequest(true);
    });
  });
}
