/** htmx.ajax for the modules that refetch fragments (chat.ts, shot.ts): failures are logged and shown, never thrown. */
import htmx from 'htmx.org';
import { createAjaxGet } from './components/lib/ajax';
import { createToast } from './components/lib/toast';

/** Shows a message in #toast-region, where the server's own toasts go. */
export function showToast(message: string): void {
  const toast = createToast(message);
  if (toast) document.getElementById('toast-region')?.append(toast);
}

export const ajaxGet = createAjaxGet((verb, url, context) => htmx.ajax(verb, url, context), showToast);
