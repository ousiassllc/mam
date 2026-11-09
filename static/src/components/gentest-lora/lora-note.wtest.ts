import { expect } from '@open-wc/testing';
import { initGentestLoRA, refreshLoRANote } from './lora-note';

// The note under the LoRA choice of the generation test (#1119): drawn from the hidden inputs of the model picker's field (backend/internal/web/handler/modelcards/lora.go writes them) and the data attributes of the
// ticked model cards (organisms/gentest/models_card.go), and the recommended strength goes on the slider until
// the user moves it.
const FORM = `
<form>
  <div data-model="m-sdxl" data-label="SDXL" data-supports-lora="true" data-lora-family="sdxl" data-lora-family-label="SDXL"><input type="checkbox" name="model" value="m-sdxl" checked></div>
  <div data-model="m-qwen" data-label="Qwen-Image 2.1" data-supports-lora="true" data-lora-family="qwen-image" data-lora-family-label="Qwen-Image"><input type="checkbox" name="model" value="m-qwen"></div>
  <div data-gt-lora>
    <div id="gt-lora-name" data-model-pick-field>
      <input type="hidden" name="lora" value="" data-mp-from="value">
      <input type="hidden" value="" data-mp-from="known">
      <input type="hidden" value="" data-mp-from="families">
      <input type="hidden" value="" data-mp-from="family_labels">
      <input type="hidden" value="" data-mp-from="strength">
      <input type="hidden" value="" data-mp-from="triggers">
      <input type="hidden" value="" data-mp-from="commercial">
    </div>
    <label><output>0.40</output><input type="range" name="lora_strength" min="0" max="1" step="0.05" value="0.4" data-gt-lora-strength></label>
  </div>
  <ul data-gt-lora-note hidden></ul>
</form>`;

describe('the note under the LoRA choice (#1119)', () => {
  let host: HTMLElement;
  const q = <T extends Element>(selector: string) => host.querySelector<T>(selector) as T;
  // What choosing a card sets in the field (modelcards.LoRAValues), then the change the picker script fires from the first input.
  const LORAS: Record<string, Record<string, string>> = {
    '': { value: '', known: '', families: '', family_labels: '', strength: '', triggers: '', commercial: '' },
    'mine.safetensors': {
      value: 'mine.safetensors',
      known: 'true',
      families: 'sdxl',
      family_labels: 'SDXL',
      strength: '0.7',
      triggers: 'mychar, red dress',
      commercial: 'unknown',
    },
    'hand.safetensors': {
      value: 'hand.safetensors',
      known: 'false',
      families: '',
      family_labels: '',
      strength: '',
      triggers: '',
      commercial: 'unknown',
    },
  };
  const choose = (value: string) => {
    for (const [key, v] of Object.entries(LORAS[value] ?? {})) q<HTMLInputElement>(`[data-mp-from="${key}"]`).value = v;
    q<HTMLInputElement>('[data-mp-from="value"]').dispatchEvent(new Event('change', { bubbles: true }));
  };
  const note = () => [...q('[data-gt-lora-note]').querySelectorAll('li')].map((li) => li.textContent);

  before(() => initGentestLoRA());

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = FORM;
    document.body.append(host);
  });
  afterEach(() => host.remove());

  it('is hidden while "なし" is chosen', () => {
    refreshLoRANote(q('#gt-lora-name'));
    expect(q<HTMLElement>('[data-gt-lora-note]').hidden).to.equal(true);
  });

  it('shows what is known of the chosen LoRA, and puts the recommended strength on the slider', () => {
    choose('mine.safetensors');
    expect(q<HTMLElement>('[data-gt-lora-note]').hidden).to.equal(false);
    expect(note()).to.include('対応するベースモデル: SDXL');
    expect(note()).to.include('トリガー語: mychar、red dress（プロンプトに入れてください）');
    expect(note()).to.include('商用利用: 不明');
    expect(q<HTMLInputElement>('[data-gt-lora-strength]').value).to.equal('0.7');
    expect(q('output').textContent).to.equal('0.70');
  });

  it('keeps the strength the user chose', () => {
    q<HTMLInputElement>('[data-gt-lora-strength]').dataset.touched = '';
    q<HTMLInputElement>('[data-gt-lora-strength]').value = '0.55';
    choose('mine.safetensors');
    expect(q<HTMLInputElement>('[data-gt-lora-strength]').value).to.equal('0.55');
  });

  it('warns when a model of another base model is ticked, and follows the ticks', () => {
    choose('mine.safetensors');
    expect(note().some((t) => t?.startsWith('⚠'))).to.equal(false);
    const qwen = q<HTMLInputElement>('input[value="m-qwen"]');
    qwen.checked = true;
    qwen.dispatchEvent(new Event('change', { bubbles: true }));
    const warns = note().filter((t) => t?.startsWith('⚠'));
    expect(warns).to.have.length(1);
    expect(warns[0]).to.contain('「Qwen-Image 2.1」は Qwen-Image 用のモデルです');
    expect(q('[data-gt-lora-note] li[role="alert"]')).to.not.equal(null);
    qwen.checked = false;
    qwen.dispatchEvent(new Event('change', { bubbles: true }));
    expect(note().some((t) => t?.startsWith('⚠'))).to.equal(false);
  });

  it('says a file the model manager did not add has nothing set', () => {
    choose('hand.safetensors');
    expect(note()).to.have.length(1);
    expect(note()[0]).to.contain('未設定');
    expect(q<HTMLInputElement>('[data-gt-lora-strength]').value).to.equal('0.4');
  });
});
