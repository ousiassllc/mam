import { describe, expect, it } from 'vitest';
import { ApiError } from './api';
import { errorMessage, requestFailureMessage } from './errors';

describe('errorMessage', () => {
  it('shows the detail of a JSON problem', () => {
    expect(errorMessage(new ApiError(409, { detail: 'すでに選択済みです' }))).toBe('すでに選択済みです');
  });

  it('names the status when the response has no readable message', () => {
    expect(errorMessage(new ApiError(503, {}))).toBe('操作に失敗しました（503）');
  });

  it('reports a failure that is not an error response as a communication failure', () => {
    expect(errorMessage(new TypeError('Failed to fetch'))).toBe('通信に失敗しました');
  });
});

describe('requestFailureMessage', () => {
  it('tells a network failure from a timeout and a response that was not a toast', () => {
    expect(requestFailureMessage('htmx:sendError')).toBe('通信に失敗しました。接続を確認してからやり直してください');
    expect(requestFailureMessage('htmx:timeout')).toBe(
      '応答に時間がかかっています。しばらくしてからやり直してください',
    );
    expect(requestFailureMessage('htmx:responseError', 500)).toBe('操作に失敗しました（500）');
    expect(requestFailureMessage('htmx:responseError')).toBe('操作に失敗しました');
  });
});
