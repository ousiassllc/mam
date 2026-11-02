import { expect, fixture, html } from '@open-wc/testing';
import './mam-prompt-weight';
import type { MamPromptWeight } from './mam-prompt-weight';

type Level = 'supported' | 'weak' | 'unsupported' | 'unknown';

const card = (
  id: string,
  level: Level,
  checked = true,
) => html`<li data-model=${id} data-label=${id} data-prompt-weight=${level} data-prompt-weight-text=${`${id} は ${level}`}>
  <input type="checkbox" name="model" value=${id} ?checked=${checked} />
</li>`;

/** The prompt fieldset of the form (gentesttier/easy.templ, organisms/gentest/compose.templ) with its models. */
async function form(text: string, levels: Level[] = ['supported'], maxlength?: number) {
  const el = await fixture<HTMLFormElement>(html`<form>
    <ul hidden>${levels.map((l, i) => card(`m${i}`, l))}</ul>
    <fieldset>
      <textarea name="prompt" maxlength=${maxlength ?? ''} .value=${text}></textarea>
      <textarea name="negative"></textarea>
      <mam-prompt-weight id="w"></mam-prompt-weight>
    </fieldset>
  </form>`);
  const weight = el.querySelector<MamPromptWeight>('mam-prompt-weight')!;
  await weight.updateComplete;
  return {
    el,
    weight,
    box: el.querySelector<HTMLTextAreaElement>('textarea[name="prompt"]')!,
    negative: el.querySelector<HTMLTextAreaElement>('textarea[name="negative"]')!,
  };
}

async function select(weight: MamPromptWeight, box: HTMLTextAreaElement, start: number, end: number) {
  box.focus();
  box.setSelectionRange(start, end);
  box.dispatchEvent(new Event('select', { bubbles: true }));
  await weight.updateComplete;
}

const button = (el: HTMLElement, dir: 'up' | 'down') =>
  el.querySelector<HTMLButtonElement>(`[data-gt-weight="${dir}"]`)!;

describe('<mam-prompt-weight>: 強める + / 弱める − for the words selected in a prompt (spec/components/gentest-tiers.md)', () => {
  it('is hidden until words are selected, and hides again when the selection is gone', async () => {
    const { weight, box } = await form('a cat on a sofa');
    expect(weight.hidden).to.equal(true);
    await select(weight, box, 2, 5);
    expect(weight.hidden).to.equal(false);
    await select(weight, box, 3, 3);
    expect(weight.hidden).to.equal(true);
  });

  it('puts (語:1.1) round the selected word, and goes on with the same selection', async () => {
    const { weight, box } = await form('a cat on a sofa');
    await select(weight, box, 2, 5);
    button(weight, 'up').click();
    await weight.updateComplete;
    expect(box.value).to.equal('a (cat:1.1) on a sofa');
    expect(box.value.slice(box.selectionStart, box.selectionEnd)).to.equal('(cat:1.1)');
    button(weight, 'up').click();
    button(weight, 'up').click();
    await weight.updateComplete;
    expect(box.value).to.equal('a (cat:1.3) on a sofa');
    expect(weight.querySelector('[data-testid="gt-weight-now"]')!.textContent).to.equal('1.3');
  });

  it('takes the brackets off when the weight is back to 1.0', async () => {
    const { weight, box } = await form('青い猫が寝ている');
    await select(weight, box, 0, 3);
    button(weight, 'down').click();
    await weight.updateComplete;
    expect(box.value).to.equal('(青い猫:0.9)が寝ている');
    button(weight, 'up').click();
    await weight.updateComplete;
    expect(box.value).to.equal('青い猫が寝ている');
    expect(box.value.slice(box.selectionStart, box.selectionEnd)).to.equal('青い猫');
  });

  it('stops at 1.5 and 0.5: the button is off at the limit', async () => {
    const { weight, box } = await form('(cat:1.4)');
    await select(weight, box, 1, 4);
    button(weight, 'up').click();
    await weight.updateComplete;
    expect(box.value).to.equal('(cat:1.5)');
    expect(button(weight, 'up').getAttribute('aria-disabled')).to.equal('true');
    button(weight, 'up').click();
    expect(box.value).to.equal('(cat:1.5)');
    expect(button(weight, 'down').getAttribute('aria-disabled')).to.equal('false');
  });

  it('works on the keyboard: Ctrl + ↑ / ↓ in the box, and the buttons can be reached with Tab', async () => {
    const { weight, box } = await form('a cat');
    await select(weight, box, 2, 5);
    box.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', ctrlKey: true, bubbles: true, cancelable: true }));
    await weight.updateComplete;
    expect(box.value).to.equal('a (cat:1.1)');
    box.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', metaKey: true, bubbles: true, cancelable: true }),
    );
    box.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', ctrlKey: true, bubbles: true, cancelable: true }),
    );
    await weight.updateComplete;
    expect(box.value).to.equal('a (cat:0.9)');
    expect(button(weight, 'up').tabIndex).to.equal(0);
  });

  it('keeps the bar while the focus is in it, and closes it when the focus goes elsewhere or on Esc', async () => {
    const { el, weight, box } = await form('a cat');
    await select(weight, box, 2, 5);
    button(weight, 'up').focus();
    await weight.updateComplete;
    expect(weight.hidden).to.equal(false);
    button(weight, 'up').click();
    await weight.updateComplete;
    expect(box.value).to.equal('a (cat:1.1)');
    expect(weight.hidden).to.equal(false); // the box did not lose its selection
    const other = document.createElement('button');
    el.append(other);
    other.focus();
    await weight.updateComplete;
    expect(weight.hidden).to.equal(true);
    await select(weight, box, 2, 5);
    box.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    await weight.updateComplete;
    expect(weight.hidden).to.equal(true);
  });

  it('does nothing for the negative prompt', async () => {
    const { weight, negative } = await form('a cat');
    negative.value = 'blurry';
    negative.focus();
    negative.setSelectionRange(0, 6);
    negative.dispatchEvent(new Event('select', { bubbles: true }));
    await weight.updateComplete;
    expect(weight.hidden).to.equal(true);
  });

  it('does not add a weight that would pass the length limit of the box', async () => {
    const { weight, box } = await form('a cat', ['supported'], 6);
    await select(weight, box, 2, 5);
    button(weight, 'up').click();
    await weight.updateComplete;
    expect(box.value).to.equal('a cat');
    expect(weight.querySelector('[data-testid="gt-weight-note"]')!.textContent).to.contain('文字数の上限');
  });

  it('turns the buttons off and says why for a model that does not read weights', async () => {
    const { weight, box } = await form('a cat', ['unsupported']);
    await select(weight, box, 2, 5);
    expect(weight.hidden).to.equal(false);
    expect(button(weight, 'up').getAttribute('aria-disabled')).to.equal('true');
    button(weight, 'up').click();
    box.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', ctrlKey: true, bubbles: true, cancelable: true }));
    await weight.updateComplete;
    expect(box.value).to.equal('a cat');
    expect(weight.querySelector('[data-testid="gt-weight-note"]')!.textContent).to.contain('m0 は unsupported');
    expect(button(weight, 'up').getAttribute('aria-describedby')).to.equal('w-note');
  });

  it('keeps the buttons on with a note for a weak or unchecked model, and none for one that reads weights', async () => {
    const weak = await form('a cat', ['weak']);
    await select(weak.weight, weak.box, 2, 5);
    expect(button(weak.weight, 'up').getAttribute('aria-disabled')).to.equal('false');
    expect(weak.weight.querySelector('[data-testid="gt-weight-note"]')!.textContent).to.contain('m0 は weak');
    const ok = await form('a cat', ['supported']);
    await select(ok.weight, ok.box, 2, 5);
    expect(ok.weight.querySelector('[data-testid="gt-weight-note"]')!.textContent!.trim()).to.equal('');
  });

  it('follows the model that is chosen: the bar turns off when the model is swapped for one that does not read weights', async () => {
    const { el, weight, box } = await form('a cat', ['supported']);
    await select(weight, box, 2, 5);
    expect(button(weight, 'up').getAttribute('aria-disabled')).to.equal('false');
    const input = el.querySelector<HTMLInputElement>('input[name="model"]')!;
    input.closest('[data-model]')!.setAttribute('data-prompt-weight', 'unsupported');
    input.dispatchEvent(new Event('change', { bubbles: true }));
    await weight.updateComplete;
    expect(button(weight, 'up').getAttribute('aria-disabled')).to.equal('true');
  });

  it('can be taken out of the page and put back (a swap) without leaving its listeners behind', async () => {
    const { el, weight, box } = await form('a cat');
    const parent = weight.parentElement!;
    weight.remove();
    await select(weight, box, 2, 5);
    expect(weight.hidden).to.equal(true);
    parent.append(weight);
    await select(weight, box, 2, 5);
    expect(weight.hidden).to.equal(false);
    button(weight, 'up').click();
    await weight.updateComplete;
    expect(box.value).to.equal('a (cat:1.1)');
    expect(el.querySelectorAll('mam-prompt-weight').length).to.equal(1);
  });

  it('draws its own bar when it comes from a history snapshot that kept the old one', async () => {
    const el = await fixture<HTMLElement>(html`<form>
      <fieldset>
        <textarea name="prompt"></textarea>
        <mam-prompt-weight id="w"></mam-prompt-weight>
      </fieldset>
    </form>`);
    const stale = document.createElement('div');
    stale.dataset.testid = 'gt-weight-bar';
    el.querySelector('mam-prompt-weight')!.append(stale);
    const copy = document.createElement('form');
    copy.innerHTML = el.innerHTML; // the element is upgraded when it is put in the page
    el.replaceWith(copy);
    const weight = copy.querySelector<MamPromptWeight>('mam-prompt-weight')!;
    await weight.updateComplete;
    expect(weight.querySelectorAll('[data-testid="gt-weight-bar"]').length).to.equal(0);
  });
});
