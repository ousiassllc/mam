import { expect } from '@open-wc/testing';
import { lostFocus } from './lit-focus';

describe('lit-focus: lostFocus (spec §5.2)', () => {
  let button: HTMLButtonElement;
  beforeEach(() => {
    button = document.createElement('button');
    document.body.append(button);
  });
  afterEach(() => button.remove());

  it('is false while an enabled control holds the focus', () => {
    button.focus();
    expect(lostFocus()).to.equal(false);
  });

  // Chrome 155 keeps a just-disabled button as `document.activeElement` for a while before it drops the focus to <body>.
  it('is true once the control that holds the focus is disabled, whether or not Chrome has dropped the focus yet', () => {
    button.focus();
    button.disabled = true;
    expect(lostFocus()).to.equal(true);
  });

  it('is true on <body>', () => {
    button.focus();
    button.blur();
    expect(lostFocus()).to.equal(true);
  });
});
