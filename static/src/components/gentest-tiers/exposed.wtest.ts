import { expect } from '@open-wc/testing';
import { initGentestExposed, syncExposed, tickedModels } from './exposed';

// The exposed inputs of the generation test form (#977): organisms/gentestadv/exposed.templ.
const FORM = `
<form>
  <ul>
    <li data-model="a"><input type="checkbox" name="model" value="a" checked></li>
    <li data-model="b"><input type="checkbox" name="model" value="b"></li>
  </ul>
  <fieldset data-gt-exposed-model="a"><input name="exposed.3.denoise" value="0.5"></fieldset>
  <fieldset data-gt-exposed-model="b" hidden><select name="exposed.3.sampler_name" disabled><option value="">既定</option></select></fieldset>
</form>`;

describe('the exposed inputs follow the models that are ticked', () => {
  let host: HTMLElement;
  before(() => initGentestExposed());
  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = FORM;
    document.body.append(host);
  });
  afterEach(() => host.remove());

  const group = (id: string) => host.querySelector<HTMLElement>(`[data-gt-exposed-model="${id}"]`)!;
  const field = (id: string) => group(id).querySelector<HTMLInputElement>('[name]')!;

  it('reads the ticked models, not the ones the commercial filter disabled', () => {
    host.querySelector<HTMLInputElement>('input[value="b"]')!.checked = true;
    expect([...tickedModels(host)].sort()).to.deep.equal(['a', 'b']);
    host.querySelector<HTMLInputElement>('input[value="b"]')!.disabled = true;
    expect([...tickedModels(host)]).to.deep.equal(['a']);
  });

  it('shows the group of a ticked model with its fields on, and hides the others with their fields off (they post nothing)', () => {
    syncExposed(host);
    expect(group('a').hidden).to.equal(false);
    expect(field('a').disabled).to.equal(false);
    expect(group('b').hidden).to.equal(true);
    expect(field('b').disabled).to.equal(true);
  });

  it('moves the groups when a model is ticked or unticked', () => {
    const b = host.querySelector<HTMLInputElement>('input[value="b"]')!;
    b.checked = true;
    b.dispatchEvent(new Event('change', { bubbles: true }));
    expect(group('b').hidden).to.equal(false);
    expect(field('b').disabled).to.equal(false);
    const a = host.querySelector<HTMLInputElement>('input[value="a"]')!;
    a.checked = false;
    a.dispatchEvent(new Event('change', { bubbles: true }));
    expect(group('a').hidden).to.equal(true);
    expect(field('a').disabled).to.equal(true);
  });

  it('follows a form that arrives in a swap', () => {
    host.querySelector<HTMLInputElement>('input[value="a"]')!.checked = false;
    host.querySelector<HTMLInputElement>('input[value="b"]')!.checked = true;
    host.dispatchEvent(new Event('htmx:load', { bubbles: true }));
    expect(group('a').hidden).to.equal(true);
    expect(group('b').hidden).to.equal(false);
  });
});
