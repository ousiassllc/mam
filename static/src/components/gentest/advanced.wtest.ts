import { expect } from '@open-wc/testing';
import { initGentestAdvanced } from './advanced';

// The "詳細設定" section of the generation test form (#973). The data attributes are those of advanced.templ and models.templ
// (backend/internal/web/handler/gentest/composetest checks the page still has them).
const FORM = `
<form>
  <ul>
    <li data-model="sdxl" data-label="SDXL" data-sampling="steps cfg sampler scheduler" data-supports-seed="true" data-default-steps="25" data-default-cfg="7">
      <input type="checkbox" name="model" value="sdxl" checked>
    </li>
    <li data-model="cloud" data-label="Cloud" data-sampling="" data-supports-seed="false">
      <input type="checkbox" name="model" value="cloud">
    </li>
  </ul>
  <details data-gt-advanced open>
    <div data-gt-adv-field="seed"><input type="number" name="seed" value=""></div>
    <div data-gt-adv-field="steps">
      <input type="checkbox" data-gt-adv-on>
      <input type="range" name="steps" min="1" max="150" value="10" data-gt-adv-range disabled>
      <output data-gt-adv-out>既定</output>
    </div>
    <div data-gt-adv-field="sampler"><select name="sampler"><option value="">既定</option><option value="euler">euler</option></select></div>
    <p data-gt-adv-note hidden></p>
  </details>
</form>`;

describe('the detailed settings of the generation test form (spec/components/gentest-screen.md)', () => {
  let host: HTMLElement;
  const q = <T extends Element>(selector: string) => host.querySelector<T>(selector)!;
  const tick = (el: HTMLInputElement, on: boolean) => {
    el.checked = on;
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };

  before(() => initGentestAdvanced());

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = FORM;
    document.body.append(host);
    host.dispatchEvent(new Event('htmx:load', { bubbles: true }));
  });

  afterEach(() => host.remove());

  it('keeps the steps off (and posting nothing) until they are switched on, then starts from the default of the model', () => {
    const range = q<HTMLInputElement>('[data-gt-adv-range]');
    expect(range.disabled).to.equal(true);
    expect(q('[data-gt-adv-out]').textContent).to.equal('既定');
    tick(q<HTMLInputElement>('[data-gt-adv-on]'), true);
    expect(range.disabled).to.equal(false);
    expect(range.value).to.equal('25');
    expect(q('[data-gt-adv-out]').textContent).to.equal('25');
    range.value = '30';
    range.dispatchEvent(new Event('input', { bubbles: true }));
    expect(q('[data-gt-adv-out]').textContent).to.equal('30');
  });

  it('hides and disables what no ticked model takes, and names the model that does not take what was given', () => {
    tick(q<HTMLInputElement>('[data-gt-adv-on]'), true);
    tick(q<HTMLInputElement>('input[value="cloud"]'), true);
    expect(q<HTMLElement>('[data-gt-adv-note]').hidden).to.equal(false);
    expect(q('[data-gt-adv-note]').textContent).to.contain('Cloud');
    tick(q<HTMLInputElement>('input[value="sdxl"]'), false);
    expect(q<HTMLElement>('[data-gt-adv-field="steps"]').hidden).to.equal(true);
    expect(q<HTMLInputElement>('[data-gt-adv-range]').disabled).to.equal(true);
    expect(q<HTMLElement>('[data-gt-adv-field="seed"]').hidden).to.equal(true);
    expect(q<HTMLSelectElement>('select[name="sampler"]').disabled).to.equal(true);
  });
});
