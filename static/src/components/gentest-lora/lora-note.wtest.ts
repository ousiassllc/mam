import { expect } from '@open-wc/testing';
import { initGentestLoRA, refreshLoRANote } from './lora-note';

// The note under the LoRA choice of the generation test (#1119): drawn from the data attributes of the chosen <option> and of the ticked model cards
// (backend/internal/web/molecules/loraselect and organisms/gentest/models_card.go write them), and the recommended strength goes on the slider until
// the user moves it.
const FORM = `
<form>
  <div data-model="m-sdxl" data-label="SDXL" data-supports-lora="true" data-lora-family="sdxl" data-lora-family-label="SDXL"><input type="checkbox" name="model" value="m-sdxl" checked></div>
  <div data-model="m-qwen" data-label="Qwen-Image 2.1" data-supports-lora="true" data-lora-family="qwen-image" data-lora-family-label="Qwen-Image"><input type="checkbox" name="model" value="m-qwen"></div>
  <div data-gt-lora>
    <select id="gt-lora-name" name="lora">
      <option value="">なし</option>
      <option value="mine.safetensors" data-known="true" data-families="sdxl" data-family-labels="SDXL" data-strength="0.7" data-triggers="mychar, red dress" data-commercial="unknown">mine</option>
      <option value="hand.safetensors" data-known="false" data-families="" data-family-labels="" data-strength="" data-triggers="" data-commercial="unknown">hand</option>
    </select>
    <label><output>0.40</output><input type="range" name="lora_strength" min="0" max="1" step="0.05" value="0.4" data-gt-lora-strength></label>
  </div>
  <ul data-gt-lora-note hidden></ul>
</form>`;

describe('the note under the LoRA choice (#1119)', () => {
  let host: HTMLElement;
  const q = <T extends Element>(selector: string) => host.querySelector<T>(selector) as T;
  const choose = (value: string) => {
    const select = q<HTMLSelectElement>('#gt-lora-name');
    select.value = value;
    select.dispatchEvent(new Event('change', { bubbles: true }));
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
