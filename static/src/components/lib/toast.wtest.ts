import { aTimeout, expect } from '@open-wc/testing';
import { createToast, dismissToastsOnClick } from './toast';

describe('error toast', () => {
  let region: HTMLElement;
  let root: HTMLElement;
  beforeEach(() => {
    root = document.createElement('div');
    region = document.createElement('div');
    root.append(region);
    document.body.append(root);
    dismissToastsOnClick(root);
  });
  afterEach(() => root.remove());

  it('stays until the close button is pressed', async () => {
    region.append(createToast('予算を超えています'));
    region.append(createToast('LOCK されています'));
    await aTimeout(200);
    expect(region.querySelectorAll('[data-toast]').length).to.equal(2);
    expect(region.querySelector('[data-toast]')!.getAttribute('role')).to.equal('alert');

    (region.querySelector('[data-toast-close]') as HTMLButtonElement).click();
    expect(region.querySelectorAll('[data-toast]').length).to.equal(1);
    expect(region.textContent).to.contain('LOCK されています');
    expect(region.textContent).not.to.contain('予算を超えています');
  });

  it('closes a toast that the server swapped in (markup of atoms/toast.templ)', () => {
    region.innerHTML = '<div role="alert" data-toast><span>エラー</span><button type="button" aria-label="閉じる" data-toast-close>×</button></div>';
    (region.querySelector('[data-toast-close]') as HTMLButtonElement).click();
    expect(region.children.length).to.equal(0);
  });
});
