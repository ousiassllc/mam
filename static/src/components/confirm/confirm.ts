/**
 * The confirmation modal (the Ousiass UI guide, O2): what is deleted, removed or cannot be taken back is asked in a centred modal with a scrim that
 * closes it, not in the browser's own confirm() box. The markup is defined once, in molecules/modal (modal.Confirm); the page shell embeds it as
 * #confirm-template and this module clones it, fills the question in and opens it with showModal() (the page behind is inert, the focus stays inside).
 *
 * - `confirmDialog(question)` asks and answers `true` (the button that goes on) or `false` (キャンセル, Esc, a click on the scrim).
 * - An element with `hx-confirm` asks the same way before its request goes out (htmx's own confirm() is taken over through `htmx:confirm`).
 * - The button that goes on is the danger button 「実行する」 (O2: the danger colour is for what cannot be taken back). A question that is not about a
 *   destruction says so on the element that asks: `data-confirm-tone="primary"` and `data-confirm-label="コピーする"` give a primary button with that word.
 */
import { whenFaded } from '../lib/ui/fade';
import { createLogger } from '../lib/logger';

const log = createLogger('confirm');
const TEMPLATE_ID = 'confirm-template';

/** How the button that goes on looks and what it says: a danger button (the template's own word) unless the asker says otherwise. */
export interface ConfirmButton {
  tone?: 'danger' | 'primary';
  label?: string;
}

/** Asks `question` in the confirmation modal; resolves `true` when the user goes on. A page without #confirm-template cannot ask, so it does not go on. */
export function confirmDialog(question: string, button: ConfirmButton = {}): Promise<boolean> {
  const template = document.getElementById(TEMPLATE_ID);
  const dialog = template instanceof HTMLTemplateElement ? template.content.firstElementChild?.cloneNode(true) : null;
  if (!(dialog instanceof HTMLDialogElement)) {
    log.warn(`#${TEMPLATE_ID} is missing; cannot ask`, question);
    return Promise.resolve(false);
  }
  const message = dialog.querySelector('[data-confirm-message]');
  if (message) message.textContent = question;
  // The template has the button in both tones (modal.Confirm); the one that is not asked for goes.
  const tone = button.tone ?? 'danger';
  for (const candidate of dialog.querySelectorAll<HTMLElement>('[data-confirm-tone]')) {
    if (candidate.dataset.confirmTone === tone) candidate.hidden = false;
    else candidate.remove();
  }
  const ok = dialog.querySelector('[data-confirm-ok]');
  if (ok && button.label) ok.textContent = button.label;
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
      // The modal and its scrim fade out (motion.css, M5): it leaves the document when the fade is over. The answer does not wait for it.
      void whenFaded(dialog).then(() => dialog.remove());
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
    const detail = (
      event as CustomEvent<{ question?: string | null; issueRequest: (skip: boolean) => void; elt?: HTMLElement }>
    ).detail;
    if (!detail.question) return;
    event.preventDefault();
    const { confirmTone, confirmLabel } = detail.elt?.dataset ?? {};
    const button = { tone: confirmTone === 'primary' ? ('primary' as const) : undefined, label: confirmLabel };
    void confirmDialog(detail.question, button).then((yes) => {
      if (yes) detail.issueRequest(true);
    });
  });
}
