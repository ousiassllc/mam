/**
 * Error toasts (#toast-region). They stay until the user closes them: an error message (a LOCK
 * violation, an exceeded budget) may be long, and a toast that vanishes on a timer cannot be read
 * at the reader's own pace or by a screen reader (WCAG 2.2.1). The markup is defined once, in
 * atoms/toast.templ; the page shell embeds it as #toast-template and this module clones it.
 */
import { createLogger } from './logger';

const log = createLogger('toast');
const CLOSE = '[data-toast-close]';

/** An error toast for `message`, not yet in the document; null when the page has no #toast-template. */
export function createToast(message: string): HTMLElement | null {
  const template = document.getElementById('toast-template');
  const toast = template instanceof HTMLTemplateElement ? template.content.firstElementChild?.cloneNode(true) : null;
  if (!(toast instanceof HTMLElement)) {
    log.warn('#toast-template is missing; cannot show', message);
    return null;
  }
  const text = toast.querySelector('[data-toast-message]');
  if (text) text.textContent = message;
  return toast;
}

/** Makes the close button of any toast, present or added later, remove that toast. */
export function dismissToastsOnClick(root: Document | HTMLElement = document): void {
  root.addEventListener('click', (event) => {
    const button = event.target instanceof Element ? event.target.closest(CLOSE) : null;
    button?.closest('[data-toast]')?.remove();
  });
}
