import { expect } from '@open-wc/testing';
import { ApiError } from './api';
import { errorMessage } from './errors';

// What an action's failure answers: atoms/toast.templ's ToastOOB, close button included.
const TOAST_OOB =
  '<div id="toast-region" hx-swap-oob="beforeend"><div class="pointer-events-auto flex items-start gap-3" role="alert" data-toast><span data-toast-message>書き出しに失敗しました</span><button type="button" class="ml-auto leading-none" aria-label="閉じる" data-toast-close>×</button></div></div>';

describe('errorMessage with the real toast markup', () => {
  it('is the message alone, without the close button', () => {
    expect(errorMessage(new ApiError(409, TOAST_OOB))).to.equal('書き出しに失敗しました');
  });

  it('falls back to the whole body when there is no message element', () => {
    expect(errorMessage(new ApiError(500, '<div data-toast>サーバーエラー</div>'))).to.equal('サーバーエラー');
  });
});
