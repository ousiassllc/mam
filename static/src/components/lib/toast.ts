/**
 * Error toasts (#toast-region). They stay until the user closes them: an error message (a LOCK
 * violation, an exceeded budget) may be long, and a toast that vanishes on a timer cannot be read
 * at the reader's own pace or by a screen reader (WCAG 2.2.1). The server renders the same markup
 * (atoms/toast.templ).
 */
const CLOSE = '[data-toast-close]';

export const TOAST_CLASS = 'pointer-events-auto flex items-start gap-3 rounded-md border border-red-500/50 bg-neutral-900 px-4 py-2 text-sm text-red-200 shadow-lg';

/** An error toast for `message`, not yet in the document. */
export function createToast(message: string): HTMLElement {
  const toast = document.createElement('div');
  toast.className = TOAST_CLASS;
  toast.setAttribute('role', 'alert');
  toast.dataset.toast = '';
  const text = document.createElement('span');
  text.textContent = message;
  const close = document.createElement('button');
  close.type = 'button';
  close.textContent = '×';
  close.setAttribute('aria-label', '閉じる');
  close.dataset.toastClose = '';
  close.className = 'ml-auto leading-none hover:text-red-100';
  toast.append(text, close);
  return toast;
}

/** Makes the close button of any toast, present or added later, remove that toast. */
export function dismissToastsOnClick(root: Document | HTMLElement = document): void {
  root.addEventListener('click', (event) => {
    const button = event.target instanceof Element ? event.target.closest(CLOSE) : null;
    button?.closest('[data-toast]')?.remove();
  });
}
