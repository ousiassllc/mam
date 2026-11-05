/**
 * The toast (#toast-region; the Ousiass UI guide, O3): a short line of what worked ("保存しました"), a pill at the bottom centre, one at a time (a new one
 * replaces the one that is up) and gone after 3 seconds. Not for what went wrong: an error, a refusal and anything that has to be read again is an alert
 * (lib/alert.ts), which stays until it is closed. The markup is defined once, in atoms/toast.templ; the page shell embeds it as #toast-template and this
 * module clones it. A toast that the server swaps in (atoms.ToastOOB) is timed the same way.
 */
import { createLogger } from '../lib/logger';

const log = createLogger('toast');
const MESSAGE = '[data-toast-message]';
const REGION = 'toast-region';

/** How long a toast stays, in milliseconds (`--toast-duration`: 3s). */
export const TOAST_MS = 3000;

/** A toast for `message`, not yet in the document; null when the page has no #toast-template. */
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

const timers = new WeakMap<Element, number>();

/** Ends a toast: it fades out (the CSS of data-leaving) and is removed when the fade is over. */
function leave(toast: Element, fadeMs: number): void {
  toast.setAttribute('data-leaving', '');
  window.setTimeout(() => toast.remove(), fadeMs);
}

/** Starts the 3 seconds of a toast that is in the region. */
function arm(toast: Element, ms: number): void {
  const old = timers.get(toast);
  if (old !== undefined) window.clearTimeout(old);
  const fade =
    Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--motion-fade-out')) || 240;
  timers.set(
    toast,
    window.setTimeout(() => leave(toast, fade), ms),
  );
}

/** Shows `message` in `region`, in place of the toast that is up, for `ms` milliseconds. */
export function showToastIn(region: Element | null, message: string, ms = TOAST_MS): void {
  if (!region) return;
  const toast = createToast(message);
  if (!toast) return;
  region.replaceChildren(toast);
  arm(toast, ms);
}

/** Shows `message` in #toast-region. */
export function showToast(message: string): void {
  showToastIn(document.getElementById(REGION), message);
}

/**
 * Times the toast that an out-of-band response put in the region (atoms.ToastOOB sets the region's content, so the one that was up is already
 * replaced): after the swap, whatever toast is in the region is armed.
 */
export function timeToastsOnSwap(root: Document | HTMLElement = document, ms = TOAST_MS): void {
  root.addEventListener('htmx:oobAfterSwap', (event) => {
    const { target } = (event as CustomEvent<{ target?: Element }>).detail;
    if (target?.id !== REGION) return;
    for (const toast of Array.from(target.querySelectorAll('[data-toast]'))) arm(toast, ms);
  });
}
