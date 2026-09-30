/**
 * A GET request through htmx that never rejects. htmx.ajax rejects on a network error, a timeout
 * or a missing target; fired with `void` that is an unhandled rejection and the swap silently does
 * not happen. The wrapper logs it, tells the user and returns whether the request went through.
 */
import { createLogger } from './logger';

export interface AjaxContext {
  target: string;
  swap: string;
}

/** htmx.ajax, as far as this module uses it. */
export type AjaxFn = (verb: 'get', url: string, context: AjaxContext) => Promise<unknown>;

export const AJAX_FAILED_MESSAGE = '通信に失敗しました。しばらくしてからもう一度お試しください。';

const log = createLogger('ajax');

export function createAjaxGet(ajax: AjaxFn, notify: (message: string) => void): (url: string, context: AjaxContext) => Promise<boolean> {
  return async (url, context) => {
    try {
      await ajax('get', url, context);
      return true;
    } catch (error) {
      log.warn(`GET ${url} failed`, error);
      notify(AJAX_FAILED_MESSAGE);
      return false;
    }
  };
}
