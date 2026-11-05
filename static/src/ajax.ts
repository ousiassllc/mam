/** htmx.ajax for the modules that refetch fragments (chat.ts, shot.ts): a failure rejects, for the Syncer that retries it. */
import htmx from 'htmx.org';
import { failOnHttpError } from './components/lib/ajax';
import { appendAlert } from './components/alert/alert';

/** Shows an error in #alert-region, where the server's own alerts go (once, while an identical one is still open). */
export function showAlert(message: string): void {
  appendAlert(document.getElementById('alert-region'), message);
}

/** htmx.ajax that also rejects on a 4xx/5xx response (see failOnHttpError). */
export const ajaxChecked = failOnHttpError(
  (verb, url, context) => htmx.ajax(verb, url, context),
  (selector) => document.querySelector(selector),
);
