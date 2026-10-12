/** htmx.ajax for the modules that refetch fragments (chat.ts, shot.ts): a failure rejects, for the Syncer that retries it. */
import htmx from 'htmx.org';
import { failOnHttpError } from './components/lib/ajax';
import { appendToast } from './components/lib/toast';

/** Shows a message in #toast-region, where the server's own toasts go (once, while an identical one is still open). */
export function showToast(message: string): void {
  appendToast(document.getElementById('toast-region'), message);
}

/** htmx.ajax that also rejects on a 4xx/5xx response (see failOnHttpError). */
export const ajaxChecked = failOnHttpError(
  (verb, url, context) => htmx.ajax(verb, url, context),
  (selector) => document.querySelector(selector),
);
