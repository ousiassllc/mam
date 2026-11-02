import { expect } from '@open-wc/testing';
import { initGentestAdvanced } from '../gentest/advanced';
import { initGentestRefine } from './refine';

// The こだわり tier of the generation test form (#977). The data attributes are those of gentesttier/refine.templ, gentestadv/fields.templ and
// refopts.templ (backend/internal/web/handler/gentest/tierformtest checks the page still has them). The form starts with the steps of the
// quality "速い" in, as the page does when the easy tier chose it.
const FORM = `
<form>
  <ul>
    <li data-model="sdxl" data-label="SDXL" data-sampling="steps cfg sampler scheduler" data-supports-seed="true" data-default-steps="25" data-default-cfg="7">
      <input type="checkbox" name="model" value="sdxl" checked>
    </li>
  </ul>
  <section data-gt-refine>
    <button type="button" hidden data-gt-reset-all>全部を元に戻す</button>
    <p data-gt-refine-empty>変えた項目はまだありません</p>
    <ul data-gt-refine-list></ul>
  </section>
  <div data-testid="gt-prompt">
    <textarea name="prompt">a fox</textarea>
    <textarea name="negative"></textarea>
  </div>
  <details data-gt-advanced open>
    <div data-gt-adv-field="seed"><input type="number" name="seed" value=""></div>
    <div data-gt-adv-field="steps">
      <input type="checkbox" data-gt-adv-on checked>
      <input type="range" name="steps" min="1" max="150" value="15" data-gt-adv-range>
      <output data-gt-adv-out>15</output>
    </div>
    <div data-gt-adv-field="sampler"><select name="sampler"><option value="">既定</option><option value="euler">euler</option></select></div>
    <p data-gt-adv-note hidden></p>
  </details>
</form>`;

describe('the こだわり tier marks what was changed and puts it back (spec/components/gentest-tiers.md)', () => {
  let host: HTMLElement;
  const q = <T extends Element>(selector: string) => host.querySelector<T>(selector)!;
  const items = () =>
    [...host.querySelectorAll<HTMLElement>('[data-gt-refine-item]')].map((li) => li.dataset.gtRefineItem);
  const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
  const type = (el: HTMLInputElement | HTMLTextAreaElement, value: string) => {
    el.value = value;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  };

  before(() => {
    initGentestAdvanced();
    initGentestRefine();
  });

  beforeEach(async () => {
    host = document.createElement('div');
    host.innerHTML = FORM;
    document.body.append(host);
    host.dispatchEvent(new Event('htmx:load', { bubbles: true }));
    await settle();
  });

  afterEach(() => host.remove());

  it('lists nothing while every control is where the tier began, with the values of the easy tier already in', () => {
    expect(q<HTMLInputElement>('[data-gt-adv-range]').value).to.equal('15');
    expect(items()).to.deep.equal([]);
    expect(q<HTMLElement>('[data-gt-refine-empty]').hidden).to.equal(false);
    expect(q<HTMLElement>('[data-gt-reset-all]').hidden).to.equal(true);
    expect(host.querySelector('[data-gt-mark]')).to.equal(null);
  });

  it('marks a changed control and lists it with what it was; changing it back clears the mark by itself', () => {
    const range = q<HTMLInputElement>('[data-gt-adv-range]');
    type(range, '30');
    expect(items()).to.deep.equal(['adv:steps']);
    expect(q('[data-gt-refine-item]').textContent).to.contain('ステップ数: 30（元は 15）');
    expect(q<HTMLElement>('[data-gt-adv-field="steps"]').hasAttribute('data-gt-changed')).to.equal(true);
    expect(q('[data-gt-adv-field="steps"] [data-gt-mark]').textContent).to.contain('変えた');
    expect(q<HTMLElement>('[data-gt-reset-all]').hidden).to.equal(false);
    type(range, '15');
    expect(items()).to.deep.equal([]);
    expect(host.querySelector('[data-gt-mark]')).to.equal(null);
  });

  it('puts one item back to where the tier began, and leaves the others as they were', () => {
    type(q<HTMLInputElement>('[data-gt-adv-range]'), '40');
    type(q<HTMLInputElement>('input[name="seed"]'), '123');
    type(q<HTMLTextAreaElement>('textarea[name="negative"]'), 'blurry');
    expect(items()).to.deep.equal(['adv:seed', 'adv:steps', 'negative:0']);
    q<HTMLButtonElement>('[data-gt-reset="adv:steps"]').click();
    expect(q<HTMLInputElement>('[data-gt-adv-range]').value).to.equal('15');
    expect(items()).to.deep.equal(['adv:seed', 'negative:0']);
    expect(q<HTMLInputElement>('input[name="seed"]').value).to.equal('123');
  });

  it('puts every item back with the one button, and a steps slider that was switched off comes back on', () => {
    const on = q<HTMLInputElement>('[data-gt-adv-on]');
    on.checked = false;
    on.dispatchEvent(new Event('change', { bubbles: true }));
    type(q<HTMLInputElement>('input[name="seed"]'), '7');
    select(q<HTMLSelectElement>('select[name="sampler"]'), 'euler');
    expect(items()).to.deep.equal(['adv:seed', 'adv:steps', 'adv:sampler']);
    expect(q('[data-gt-refine-item][data-gt-refine-item="adv:steps"]').textContent).to.contain(
      'ステップ数: 既定（元は 15）',
    );
    q<HTMLButtonElement>('[data-gt-reset-all]').click();
    expect(items()).to.deep.equal([]);
    expect(on.checked).to.equal(true);
    expect(q<HTMLInputElement>('[data-gt-adv-range]').value).to.equal('15');
    expect(q<HTMLInputElement>('input[name="seed"]').value).to.equal('');
    expect(q<HTMLSelectElement>('select[name="sampler"]').value).to.equal('');
  });
});

function select(el: HTMLSelectElement, value: string): void {
  el.value = value;
  el.dispatchEvent(new Event('change', { bubbles: true }));
}
