import { expect, fixture, html } from '@open-wc/testing';
import './mam-prompt-weight';
import type { MamPromptWeight } from './mam-prompt-weight';

type Level = 'supported' | 'weak' | 'unsupported' | 'unknown';

const card = (
  id: string,
  level: Level,
  checked = true,
) => html`<li data-model=${id} data-label=${id} data-prompt-weight=${level} data-prompt-weight-title=${`${id} は ${level}`}>
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

/** The bar body (the `role="group"`) is shown; the element itself is never hidden (W8). */
const shown = (el: HTMLElement) => !el.querySelector<HTMLElement>('[role="group"]')!.hidden;

describe('<mam-prompt-weight>: 強める + / 弱める − for the words selected in a prompt (spec/components/gentest-tiers.md)', () => {
  it('is hidden until words are selected, and hides again when the selection is gone', async () => {
    const { weight, box } = await form('a cat on a sofa');
    expect(shown(weight)).to.equal(false);
    await select(weight, box, 2, 5);
    expect(shown(weight)).to.equal(true);
    await select(weight, box, 3, 3);
    expect(shown(weight)).to.equal(false);
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
    expect(shown(weight)).to.equal(true);
    button(weight, 'up').click();
    await weight.updateComplete;
    expect(box.value).to.equal('a (cat:1.1)');
    expect(shown(weight)).to.equal(true); // the box did not lose its selection
    const other = document.createElement('button');
    el.append(other);
    other.focus();
    await weight.updateComplete;
    expect(shown(weight)).to.equal(false);
    await select(weight, box, 2, 5);
    box.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    await weight.updateComplete;
    expect(shown(weight)).to.equal(false);
  });

  it('closes on Esc from a focused button and puts the focus back in the prompt box (W7)', async () => {
    const { weight, box } = await form('a cat');
    await select(weight, box, 2, 5);
    button(weight, 'up').focus();
    expect(document.activeElement).to.equal(button(weight, 'up'));
    button(weight, 'up').dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    await weight.updateComplete;
    expect(shown(weight)).to.equal(false);
    expect(document.activeElement).to.equal(box);
    box.dispatchEvent(new KeyboardEvent('keyup', { key: 'Escape', bubbles: true })); // the release of the key does not open the bar again
    await weight.updateComplete;
    expect(shown(weight)).to.equal(false);
    await select(weight, box, 2, 5); // a new selection does
    expect(shown(weight)).to.equal(true);
  });

  it('has the role="status" note region in the DOM before the bar shows, and only its content changes (W8)', async () => {
    const { weight, box } = await form('a cat', ['unsupported']);
    const note = () => weight.querySelector<HTMLElement>('[data-testid="gt-weight-note"]');
    const before = note();
    expect(before).to.not.equal(null);
    expect(before!.getAttribute('role')).to.equal('status');
    expect(shown(weight)).to.equal(false);
    expect(weight.hidden, 'the element itself is not hidden').to.equal(false);
    // not display:none, so that it is in the accessibility tree before the bar appears (the note of a hidden element is not announced)
    expect(getComputedStyle(before!).display).to.not.equal('none');
    expect(getComputedStyle(weight).display).to.not.equal('none');
    expect(before!.closest('[role="group"]'), 'the note is not inside the bar body').to.equal(null);
    expect(before!.textContent!.trim()).to.equal('');
    await select(weight, box, 2, 5);
    expect(shown(weight)).to.equal(true);
    expect(note()).to.equal(before); // the same node: not drawn together with the bar
    expect(getComputedStyle(before!).display).to.not.equal('none');
    expect(before!.textContent).to.contain('m0');
    expect(button(weight, 'up').getAttribute('aria-describedby')).to.equal(before!.id);
  });

  it('does nothing for the negative prompt', async () => {
    const { weight, negative } = await form('a cat');
    negative.value = 'blurry';
    negative.focus();
    negative.setSelectionRange(0, 6);
    negative.dispatchEvent(new Event('select', { bubbles: true }));
    await weight.updateComplete;
    expect(shown(weight)).to.equal(false);
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
    expect(shown(weight)).to.equal(true);
    expect(button(weight, 'up').getAttribute('aria-disabled')).to.equal('true');
    button(weight, 'up').click();
    box.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', ctrlKey: true, bubbles: true, cancelable: true }));
    await weight.updateComplete;
    expect(box.value).to.equal('a cat');
    expect(weight.querySelector('[data-testid="gt-weight-note"]')!.textContent).to.contain('m0 では、ウェイト');
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
    expect(shown(weight)).to.equal(false);
    parent.append(weight);
    await select(weight, box, 2, 5);
    expect(shown(weight)).to.equal(true);
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
    expect(stale.isConnected).to.equal(false); // the kept bar is gone: the element drew its own
    expect(weight.querySelectorAll('[data-testid="gt-weight-bar"]').length).to.equal(1);
  });
});

// The bar drawn with the real Tailwind CSS of the app (the classes of the component are scanned into it).
describe('<mam-prompt-weight>: the look of the bar (spec/components/shared-ui-parts.md §1, W2 and W3)', () => {
  let stylesheet: HTMLLinkElement | undefined;
  async function tailwind() {
    stylesheet = document.createElement('link');
    stylesheet.rel = 'stylesheet';
    stylesheet.href = '/__tailwind.css';
    const loaded = new Promise<void>((resolve, reject) => {
      stylesheet!.onload = () => resolve();
      stylesheet!.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
    });
    document.head.append(stylesheet);
    await loaded;
  }
  afterEach(() => stylesheet?.remove());

  it('buttons are 32px or taller and have a ripple, the figure is monospace (W2, W3; #1094)', async function () {
    this.timeout(60_000); // the first request waits for the build of the CSS
    await tailwind();
    const { weight, box } = await form('a cat');
    await select(weight, box, 2, 5);
    for (const dir of ['up', 'down'] as const) {
      const b = button(weight, dir);
      expect(b.getAttribute('aria-disabled')).to.equal('false');
      expect(b.getBoundingClientRect().height).to.be.at.least(32);
      expect(b.hasAttribute('data-ripple')).to.equal(true);
    }
    const now = weight.querySelector<HTMLElement>('[data-testid="gt-weight-now"]')!;
    expect(now.tagName).to.equal('OUTPUT');
    expect(getComputedStyle(now).fontFamily).to.match(/mono/i);
  });
});
