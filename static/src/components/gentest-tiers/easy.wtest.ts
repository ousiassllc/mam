import { expect } from '@open-wc/testing';
import { initGentestEasy } from './easy';

// The easy tier of the generation test form (#977): gentesttier/easy.templ.
const FORM = (can: boolean) => `
<form>
  <div id="gt-easy-pick" data-can-estimate="${can}">
    <ul hidden><li data-model="m"><input type="checkbox" name="model" value="m" checked></li></ul>
  </div>
  <button type="submit" data-gt-easy-estimate ${can ? '' : 'disabled'}>見積りを見る</button>
</form>`;

describe('the easy tier keeps the estimate button and the input images in step with the model that was picked', () => {
  let host: HTMLElement;
  before(() => initGentestEasy());
  beforeEach(() => {
    host = document.createElement('div');
    document.body.append(host);
  });
  afterEach(() => host.remove());

  it('turns the estimate button off while no model was picked, and on again when one is', () => {
    host.innerHTML = FORM(false);
    host.dispatchEvent(new Event('htmx:load', { bubbles: true }));
    const button = host.querySelector<HTMLButtonElement>('[data-gt-easy-estimate]')!;
    expect(button.disabled).to.equal(true);
    const pick = host.querySelector<HTMLElement>('#gt-easy-pick')!;
    pick.dataset.canEstimate = 'true';
    pick.dispatchEvent(new Event('htmx:afterSwap', { bubbles: true }));
    expect(button.disabled).to.equal(false);
    pick.dataset.canEstimate = 'false';
    pick.dispatchEvent(new Event('htmx:afterSwap', { bubbles: true }));
    expect(button.disabled).to.equal(true);
  });

  it('tells the input images that the model changed, once the new card is in', () => {
    host.innerHTML = FORM(true);
    let heard = 0;
    host.querySelector('form')!.addEventListener('change', (e) => {
      if ((e.target as HTMLInputElement).name === 'model') heard++;
    });
    host.querySelector('#gt-easy-pick')!.dispatchEvent(new Event('htmx:afterSwap', { bubbles: true }));
    expect(heard).to.equal(1);
  });
});
