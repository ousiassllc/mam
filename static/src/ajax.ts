/** htmx.ajax for the modules that refetch fragments (chat.ts, shot.ts): failures are logged and shown, never thrown. */
import htmx from 'htmx.org';
import { createAjaxGet } from './components/lib/ajax';

/** Shows a message in #toast-region, where the server's own toasts go (main.ts removes it after a while). */
export function showToast(message: string): void {
  const region = document.getElementById('toast-region');
  if (!region) return;
  const toast = document.createElement('div');
  toast.className = 'pointer-events-auto rounded-md border border-red-500/50 bg-neutral-900 px-4 py-2 text-sm text-red-200 shadow-lg';
  toast.setAttribute('role', 'alert');
  toast.dataset.toast = '';
  toast.textContent = message;
  region.append(toast);
}

export const ajaxGet = createAjaxGet((verb, url, context) => htmx.ajax(verb, url, context), showToast);
