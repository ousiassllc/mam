/**
 * A GET request through htmx that never rejects. htmx.ajax rejects on a network error, a timeout
 * or a missing target; fired with `void` that is an unhandled rejection and the swap silently does
 * not happen. The wrapper logs it, tells the user and returns whether the request went through.
 */
import { createLogger } from './logger';
import { isSuccessResponse } from './thread';

export interface AjaxContext {
  target: string;
  swap: string;
  /**
   * Element (selector) the request is issued from. htmx runs one request per element and replaces
   * a queued one with the next: without a source every request shares <body>, so concurrent
   * refetches silently drop each other (and their promises resolve as if they had succeeded).
   * Defaults to the target, which gives each swapped region its own queue.
   */
  source?: string;
  /** Selector of the part of the response to swap in (htmx `select`); the whole response when absent. */
  select?: string;
}

/** htmx.ajax, as far as this module uses it. */
export type AjaxFn = (verb: 'get', url: string, context: AjaxContext) => Promise<unknown>;

/**
 * Wraps htmx.ajax so that a non-2xx response rejects as well. htmx.ajax resolves for 4xx/5xx (only a
 * network error, a timeout or an abort reject), and main.ts clears `isError` for HTML error responses, so
 * the status is read from the `htmx:afterRequest` of the request's source element, which fires before the
 * promise settles. `findSource` resolves the context's source/target selector to that element.
 */
export function failOnHttpError(ajax: AjaxFn, findSource: (selector: string) => EventTarget | null): AjaxFn {
  return async (verb, url, context) => {
    const source = findSource(context.source ?? context.target);
    let status: number | undefined;
    const onAfterRequest = (event: Event) => {
      const detail = (event as CustomEvent<{ xhr?: { status: number }; pathInfo?: { requestPath?: string } }>).detail;
      if (detail?.pathInfo?.requestPath === url) status = detail.xhr?.status;
    };
    source?.addEventListener('htmx:afterRequest', onAfterRequest);
    try {
      await ajax(verb, url, context);
    } finally {
      source?.removeEventListener('htmx:afterRequest', onAfterRequest);
    }
    if (status !== undefined && !isSuccessResponse({ status }))
      throw new Error(`${verb.toUpperCase()} ${url} responded ${status}`);
  };
}

export const AJAX_FAILED_MESSAGE = '通信に失敗しました。しばらくしてからもう一度お試しください。';

const log = createLogger('ajax');

export function createAjaxGet(
  ajax: AjaxFn,
  notify: (message: string) => void,
): (url: string, context: AjaxContext) => Promise<boolean> {
  return async (url, context) => {
    try {
      await ajax('get', url, { source: context.target, ...context });
      return true;
    } catch (error) {
      log.warn(`GET ${url} failed`, error);
      notify(AJAX_FAILED_MESSAGE);
      return false;
    }
  };
}
