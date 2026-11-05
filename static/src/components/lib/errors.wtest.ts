import { expect } from '@open-wc/testing';
import { ApiError } from './api';
import { errorMessage } from './errors';

// What an action's failure answers: atoms/alert.templ's ErrorAlertOOB, heading and close button included.
const ALERT_OOB =
  '<div id="alert-region" hx-swap-oob="beforeend"><div class="pointer-events-auto flex items-start gap-3" role="alert" data-alert><span data-alert-message>書き出しに失敗しました</span><button type="button" class="ml-auto leading-none" aria-label="閉じる" data-alert-close>×</button></div></div>';

describe('errorMessage with the real alert markup', () => {
  it('is the message alone, without the close button', () => {
    expect(errorMessage(new ApiError(409, ALERT_OOB))).to.equal('書き出しに失敗しました');
  });

  it('falls back to the whole body when there is no message element', () => {
    expect(errorMessage(new ApiError(500, '<div data-alert>サーバーエラー</div>'))).to.equal('サーバーエラー');
  });
});
