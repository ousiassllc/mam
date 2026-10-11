import { ApiError } from './api';

/** The message of an action's error response: a toast fragment (HTML) or a JSON problem. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const body = error.body;
    if (typeof body === 'string') {
      // A toast fragment also holds the close button ("×"): take the message element alone.
      const { body: doc } = new DOMParser().parseFromString(body, 'text/html');
      const text = (doc.querySelector('[data-toast-message]') ?? doc).textContent?.trim();
      if (text) return text;
    } else if (body && typeof body === 'object' && 'detail' in body && typeof body.detail === 'string') {
      return body.detail;
    }
    return `操作に失敗しました（${error.status}）`;
  }
  return '通信に失敗しました';
}

/** The htmx events that report a request that produced no swappable response. */
export type RequestFailure = 'htmx:responseError' | 'htmx:sendError' | 'htmx:timeout';

/** What to tell the user about a failed htmx request: the server was unreachable, too slow, or answered with something that is not a toast. */
export function requestFailureMessage(event: RequestFailure, status?: number): string {
  switch (event) {
    case 'htmx:sendError':
      return '通信に失敗しました。接続を確認してからやり直してください';
    case 'htmx:timeout':
      return '応答に時間がかかっています。しばらくしてからやり直してください';
    case 'htmx:responseError':
      return status ? `操作に失敗しました（${status}）` : '操作に失敗しました';
  }
}
