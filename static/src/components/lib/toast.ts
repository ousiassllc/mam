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
  const text = toast.querySelector(MESSAGE);
  if (text) text.textContent = message;
  return toast;
}

const MESSAGE = '[data-toast-message]';

function messageOf(toast: Element): string {
  return toast.querySelector(MESSAGE)?.textContent ?? '';
}

/** Whether `region` already shows a toast with this message. */
export function hasToast(region: Element, message: string): boolean {
  return Array.from(region.querySelectorAll('[data-toast]')).some((toast) => messageOf(toast) === message);
}

/**
 * Appends an error toast for `message` unless the region already shows one with the same message: a
 * failure that repeats (the 15s Cost Summary poll while the server is down) must not pile up toasts that
 * stay until closed one by one.
 */
export function appendToast(region: Element | null, message: string): void {
  if (!region || hasToast(region, message)) return;
  const toast = createToast(message);
  if (toast) region.append(toast);
}

/**
 * Drops, from an out-of-band response about to be swapped into #toast-region, the toasts that the
 * region already shows (same rule as appendToast, for toasts the server sends).
 */
export function dedupeToastsOnSwap(root: Document | HTMLElement = document): void {
  root.addEventListener('htmx:oobBeforeSwap', (event) => {
    const { target, fragment } = (event as CustomEvent<{ target?: Element; fragment?: ParentNode }>).detail;
    if (target?.id !== 'toast-region' || !fragment) return;
    const seen = new Set(Array.from(target.querySelectorAll('[data-toast]')).map(messageOf));
    for (const toast of Array.from(fragment.querySelectorAll('[data-toast]'))) {
      const message = messageOf(toast);
      if (seen.has(message)) toast.remove();
      else seen.add(message);
    }
  });
}

/** Makes the close button of any toast, present or added later, remove that toast. */
export function dismissToastsOnClick(root: Document | HTMLElement = document): void {
  root.addEventListener('click', (event) => {
    const button = event.target instanceof Element ? event.target.closest(CLOSE) : null;
    button?.closest('[data-toast]')?.remove();
  });
}
