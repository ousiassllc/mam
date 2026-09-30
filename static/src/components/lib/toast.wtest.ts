import { aTimeout, expect } from '@open-wc/testing';
import { createToast, dismissToastsOnClick } from './toast';

// The markup of atoms/toast.templ, as AppLayout embeds it in #toast-template.
const TOAST =
  '<div class="pointer-events-auto flex" role="alert" data-toast><span data-toast-message></span><button type="button" class="ml-auto" aria-label="閉じる" data-toast-close>×</button></div>';

describe('error toast', () => {
  let region: HTMLElement;
  let root: HTMLElement;
  let template: HTMLTemplateElement;
  beforeEach(() => {
    root = document.createElement('div');
    region = document.createElement('div');
    template = document.createElement('template');
    template.id = 'toast-template';
    template.innerHTML = TOAST;
    root.append(region, template);
    document.body.append(root);
    dismissToastsOnClick(root);
  });
  afterEach(() => root.remove());

  it('is a clone of the template with only the message filled in', () => {
    const toast = createToast('予算を超えています')!;
    expect(toast.querySelector('[data-toast-message]')!.textContent).to.equal('予算を超えています');
    toast.querySelector('[data-toast-message]')!.textContent = '';
    expect(toast.outerHTML).to.equal(template.content.firstElementChild!.outerHTML);
  });

  it('shows the message as text, not markup', () => {
    const toast = createToast('<img src=x>')!;
    expect(toast.querySelector('img')).to.equal(null);
    expect(toast.textContent).to.contain('<img src=x>');
  });

  it('does not change the template for the next toast', () => {
    createToast('first');
    expect(template.content.querySelector('[data-toast-message]')!.textContent).to.equal('');
  });

  it('returns null when the page has no template', () => {
    template.remove();
    expect(createToast('x')).to.equal(null);
  });

  it('stays until the close button is pressed', async () => {
    region.append(createToast('予算を超えています')!);
    region.append(createToast('LOCK されています')!);
    await aTimeout(200);
    expect(region.querySelectorAll('[data-toast]').length).to.equal(2);
    expect(region.querySelector('[data-toast]')!.getAttribute('role')).to.equal('alert');

    (region.querySelector('[data-toast-close]') as HTMLButtonElement).click();
    expect(region.querySelectorAll('[data-toast]').length).to.equal(1);
    expect(region.textContent).to.contain('LOCK されています');
    expect(region.textContent).not.to.contain('予算を超えています');
  });

  it('closes a toast that the server swapped in', () => {
    region.innerHTML = TOAST;
    (region.querySelector('[data-toast-close]') as HTMLButtonElement).click();
    expect(region.children.length).to.equal(0);
  });
});
