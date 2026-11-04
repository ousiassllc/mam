import { expect, fixture, html } from '@open-wc/testing';
import './mam-weight-help';
import './mam-weight-note';
import type { MamWeightHelp } from './mam-weight-help';
import type { MamWeightNote } from './mam-weight-note';

type Level = 'supported' | 'weak' | 'unsupported' | 'unknown';

const TITLES: Record<Level, string> = {
  supported: '効きます',
  weak: '効きが弱いかも',
  unsupported: '効きません（代わりに文で強調してください）',
  unknown: '未確認',
};

/** A model card as the form draws it (organisms/gentest/models_card.go): the display name, never the identifier, and the short words of the weights. */
const card = (id: string, label: string, level: Level, checked: boolean) => html`<li
  data-model=${id}
  data-label=${label}
  data-prompt-weight=${level}
  data-prompt-weight-title=${TITLES[level]}
>
  <input type="checkbox" name="model" value=${id} ?checked=${checked} />
</li>`;

const MODELS: [string, string, Level][] = [
  ['comfyui:bundled-comfyui/sdxl-base', 'SDXL base', 'supported'],
  ['comfyui:bundled-comfyui/flux1-dev', 'FLUX.1 dev', 'weak'],
  ['comfyui:bundled-comfyui/qwen-image', 'Qwen-Image', 'unsupported'],
  ['comfyui:bundled-comfyui/noctq-v4-qwen-image21', 'Noct Q V4（Qwen-Image 2.1）', 'unsupported'],
  ['nano-banana', 'Nano Banana', 'unknown'],
];

/** The form of the generation test: every model has a card, the ticked ones are the chosen ones; the help and the caution stand in the prompt fieldset. */
async function form(chosen: string[]) {
  const el = await fixture<HTMLFormElement>(html`<form>
    <ul hidden>${MODELS.map(([id, label, level]) => card(id, label, level, chosen.includes(label)))}</ul>
    <fieldset>
      <mam-weight-help></mam-weight-help>
      <mam-weight-note hidden></mam-weight-note>
    </fieldset>
  </form>`);
  const help = el.querySelector<MamWeightHelp>('mam-weight-help')!;
  const note = el.querySelector<MamWeightNote>('mam-weight-note')!;
  await Promise.all([help.updateComplete, note.updateComplete]);
  return { el, help, note };
}

const lines = (help: Element) => [...help.querySelectorAll('li')].map((li) => li.textContent!.trim());

async function tick(
  el: HTMLFormElement,
  label: string,
  on: boolean,
  ...elements: { updateComplete: Promise<boolean> }[]
) {
  const input = [...el.querySelectorAll<HTMLInputElement>('input[name="model"]')].find(
    (i) => i.closest<HTMLElement>('[data-model]')!.dataset.label === label,
  )!;
  input.checked = on;
  input.dispatchEvent(new Event('change', { bubbles: true }));
  await Promise.all(elements.map((e) => e.updateComplete));
}

describe('<mam-weight-help>: the "?" of the prompt weights says what the chosen models do (Issue #1023)', () => {
  it('is one line for one chosen model: its display name and the short words, and no identifier', async () => {
    const { help } = await form(['Qwen-Image']);
    expect(lines(help)).to.deep.equal(['Qwen-Image: 効きません（代わりに文で強調してください）']);
    expect(help.textContent).not.to.contain('comfyui:');
    expect(help.textContent).not.to.contain('bundled-comfyui');
  });

  it('names only the chosen models, the ones that read the weights the same way on one line, what works first', async () => {
    const { help } = await form(['Nano Banana', 'Qwen-Image', 'Noct Q V4（Qwen-Image 2.1）', 'SDXL base']);
    expect(lines(help)).to.deep.equal([
      'SDXL base: 効きます',
      'Qwen-Image・Noct Q V4（Qwen-Image 2.1）: 効きません（代わりに文で強調してください）',
      'Nano Banana: 未確認',
    ]);
    expect(help.textContent).not.to.contain('FLUX.1 dev');
    expect(help.textContent).not.to.contain('comfyui:');
  });

  it('says a model that is not known in one word, and never repeats a sentence', async () => {
    const { help } = await form(['Nano Banana']);
    expect(lines(help)).to.deep.equal(['Nano Banana: 未確認']);
    expect(help.textContent).not.to.contain('確かめていません');
  });

  it('follows the choice: a model ticked is added and one unticked is gone', async () => {
    const { el, help, note } = await form(['SDXL base']);
    expect(lines(help)).to.deep.equal(['SDXL base: 効きます']);
    await tick(el, 'FLUX.1 dev', true, help, note);
    expect(lines(help)).to.deep.equal(['SDXL base: 効きます', 'FLUX.1 dev: 効きが弱いかも']);
    await tick(el, 'SDXL base', false, help, note);
    expect(lines(help)).to.deep.equal(['FLUX.1 dev: 効きが弱いかも']);
  });

  it('says what to do while no model is chosen', async () => {
    const { help } = await form([]);
    expect(help.querySelectorAll('li').length).to.equal(0);
    expect(help.querySelector('[data-testid="gt-weight-none"]')).not.to.equal(null);
  });

  it('can be taken out of the page and put back without leaving its listeners behind', async () => {
    const { el, help } = await form(['SDXL base']);
    const parent = help.parentElement!;
    help.remove();
    parent.append(help);
    await tick(el, 'Qwen-Image', true, help);
    expect(lines(help)).to.deep.equal([
      'SDXL base: 効きます',
      'Qwen-Image: 効きません（代わりに文で強調してください）',
    ]);
  });
});

describe('<mam-weight-note>: the short caution beside the prompt for a model that does not read weights (Issue #1023)', () => {
  it('is hidden while every chosen model reads the weights, reads them poorly or is not known', async () => {
    for (const chosen of [['SDXL base'], ['FLUX.1 dev'], ['Nano Banana'], []]) {
      const { note } = await form(chosen);
      expect(note.hidden, chosen.join()).to.equal(true);
      expect(note.textContent!.trim()).to.equal('');
    }
  });

  it('shows the caution with the name of the model that does not read them, and what to do instead', async () => {
    const { note } = await form(['SDXL base', 'Qwen-Image']);
    expect(note.hidden).to.equal(false);
    expect(note.textContent).to.contain('Qwen-Image');
    expect(note.textContent).not.to.contain('SDXL base');
    expect(note.textContent).to.contain('効きません');
    expect(note.textContent).to.contain('文で言い換えてください');
    expect(note.textContent).not.to.contain('comfyui:');
  });

  it('names every such model once, and comes and goes with the choice', async () => {
    const { el, help, note } = await form(['Qwen-Image']);
    await tick(el, 'Noct Q V4（Qwen-Image 2.1）', true, help, note);
    expect(note.textContent).to.contain('Qwen-Image・Noct Q V4（Qwen-Image 2.1）');
    expect(note.querySelectorAll('p').length).to.equal(1);
    await tick(el, 'Qwen-Image', false, help, note);
    await tick(el, 'Noct Q V4（Qwen-Image 2.1）', false, help, note);
    expect(note.hidden).to.equal(true);
  });

  it('draws its own note when it comes from a history snapshot that kept the old one', async () => {
    const { el } = await form(['SDXL base']);
    const stale = document.createElement('p');
    stale.textContent = 'old';
    el.querySelector('mam-weight-note')!.append(stale);
    const copy = document.createElement('form');
    copy.innerHTML = el.innerHTML;
    el.replaceWith(copy);
    const note = copy.querySelector<MamWeightNote>('mam-weight-note')!;
    await note.updateComplete;
    expect(note.textContent).not.to.contain('old');
  });
});
