import { aTimeout, expect } from '@open-wc/testing';
import { showToastIn, timeToastsOnSwap } from './toast';

// The markup of atoms/toast.templ, as AppLayout embeds it in #toast-template.
const TOAST = '<div class="pointer-events-auto flex" role="status" data-toast><span data-toast-message></span></div>';

describe('toast', () => {
  let region: HTMLElement;
  let root: HTMLElement;
  beforeEach(() => {
    root = document.createElement('div');
    region = document.createElement('div');
    region.id = 'toast-region';
    const template = document.createElement('template');
    template.id = 'toast-template';
    template.innerHTML = TOAST;
    root.append(region, template);
    document.body.append(root);
  });
  afterEach(() => root.remove());

  it('shows the message and goes by itself', async () => {
    showToastIn(region, '保存しました', 30);
    expect(region.querySelector('[data-toast-message]')!.textContent).to.equal('保存しました');
    await aTimeout(40);
    expect(region.querySelector('[data-toast]')!.hasAttribute('data-leaving')).to.equal(true);
    await aTimeout(400);
    expect(region.querySelector('[data-toast]')).to.equal(null);
  });

  it('is one at a time: a new toast replaces the one that is up', () => {
    showToastIn(region, 'first');
    showToastIn(region, 'second');
    const toasts = region.querySelectorAll('[data-toast]');
    expect(toasts.length).to.equal(1);
    expect(toasts[0]!.textContent).to.equal('second');
  });

  it('shows nothing when the page has no #toast-template', () => {
    root.querySelector('template')!.remove();
    showToastIn(region, 'x');
    expect(region.children.length).to.equal(0);
  });

  it('times the toast that the server swapped in', async () => {
    timeToastsOnSwap(root, 30);
    region.innerHTML = TOAST;
    region.dispatchEvent(new CustomEvent('htmx:oobAfterSwap', { bubbles: true, detail: { target: region } }));
    await aTimeout(40);
    expect(region.querySelector('[data-toast]')!.hasAttribute('data-leaving')).to.equal(true);
  });
});
