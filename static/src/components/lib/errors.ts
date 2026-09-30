import { ApiError } from './api';

/** The message of an action's error response: a toast fragment (HTML) or a JSON problem. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const body = error.body;
    if (typeof body === 'string') {
      const text = new DOMParser().parseFromString(body, 'text/html').body.textContent?.trim();
      if (text) return text;
    } else if (body && typeof body === 'object' && 'detail' in body && typeof body.detail === 'string') {
      return body.detail;
    }
    return `操作に失敗しました（${error.status}）`;
  }
  return '通信に失敗しました';
}
