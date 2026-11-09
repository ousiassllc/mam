import { describe, expect, it } from 'vitest';
import { commercialOf, loraNote, mismatches, strengthFor, type LoRAFacts, type ModelFamily } from './lora-note-model';

const qwen: ModelFamily = { label: 'Qwen-Image 2.1', family: 'qwen-image', familyLabel: 'Qwen-Image' };
const sdxl: ModelFamily = { label: 'SDXL', family: 'sdxl', familyLabel: 'SDXL' };
const unsaid: ModelFamily = { label: '手書きのモデル', family: '', familyLabel: '' };

const known = (over: Partial<LoRAFacts> = {}): LoRAFacts => ({
  known: true,
  families: ['sdxl'],
  familyLabels: 'SDXL',
  strength: '0.7',
  triggers: ['mychar', 'red dress'],
  commercial: 'conditional',
  ...over,
});

const texts = (lines: { text: string }[]) => lines.map((l) => l.text);

describe('the note under the LoRA choice (#1119)', () => {
  it('says nothing while no LoRA is chosen', () => {
    expect(loraNote(null, [sdxl], false)).toEqual([]);
  });

  it('shows the base model, strength, trigger words and license of a LoRA the model manager knows', () => {
    const lines = texts(loraNote(known(), [sdxl], false));
    expect(lines).toContain('対応するベースモデル: SDXL');
    expect(lines).toContain('推奨の強さ: 0.7');
    expect(lines).toContain('トリガー語: mychar、red dress（プロンプトに入れてください）');
    expect(lines).toContain('商用利用: 条件つき（利用者の自己申告）');
    expect(loraNote(known(), [sdxl], false).some((l) => l.tone === 'warn')).toBe(false);
  });

  it('warns when the base model of the LoRA is not the one of a ticked model', () => {
    const lines = loraNote(known(), [sdxl, qwen], false);
    const warns = lines.filter((l) => l.tone === 'warn');
    expect(warns).toHaveLength(1);
    expect(warns[0]?.text).toContain('「Qwen-Image 2.1」は Qwen-Image 用のモデルです');
    expect(warns[0]?.text).toContain('合わない組み合わせ');
    expect(mismatches(known(), [sdxl, qwen])).toEqual([qwen]);
  });

  it('does not call a model without a family a mismatch, nor warn for a LoRA that fits one of its families', () => {
    expect(mismatches(known(), [unsaid])).toEqual([]);
    expect(mismatches(known({ families: ['sdxl', 'qwen-image'] }), [qwen, sdxl])).toEqual([]);
  });

  it('says 未設定 for what the file does not say and cannot judge the fit; never guesses', () => {
    const lora = known({ families: [], familyLabels: '', strength: '', triggers: [], commercial: 'unknown' });
    const lines = loraNote(lora, [qwen], false);
    expect(texts(lines)).toEqual([
      '対応するベースモデル: 未設定',
      'ベースモデルが未設定のため、選んだモデルに合うか確かめられません。',
      '推奨の強さ: 未設定',
      'トリガー語: 未設定',
      '商用利用: 不明',
    ]);
    expect(mismatches(lora, [qwen])).toEqual([]);
  });

  it('says that a file nobody added through the model manager has nothing set', () => {
    const lines = loraNote({ ...known(), known: false }, [qwen], false);
    expect(lines).toHaveLength(1);
    expect(lines[0]?.text).toContain('未設定');
  });

  it('warns about a LoRA that is not 商用OK only while the setting 商用OKのモデルだけを使う is on, and does not stop anything', () => {
    const off = loraNote(known({ commercial: 'unknown' }), [sdxl], false);
    expect(off.filter((l) => l.tone === 'warn')).toHaveLength(0);
    const on = loraNote(known({ commercial: 'unknown' }), [sdxl], true);
    expect(on.filter((l) => l.tone === 'warn').map((l) => l.text)).toEqual([
      '「商用OKのモデルだけを使う」がオンですが、この LoRA は商用OKと確認できていません。生成は止めません。',
    ]);
  });

  it('reads the commercial answer without ever defaulting to 商用OK', () => {
    expect(commercialOf(undefined)).toBe('unknown');
    expect(commercialOf('')).toBe('unknown');
    expect(commercialOf('bogus')).toBe('unknown');
    expect(commercialOf('conditional')).toBe('conditional');
    expect(commercialOf('no')).toBe('no');
  });
});

describe('the strength a LoRA is chosen with', () => {
  it('is the recommended one until the user moves the slider', () => {
    expect(strengthFor('0.7', false, '0.4', '0.4')).toBe('0.7');
    expect(strengthFor('0.7', true, '0.5', '0.4')).toBe('0.5');
  });

  it('falls back to the default of the form when none is recommended or the value is not a strength', () => {
    expect(strengthFor('', false, '0.7', '0.4')).toBe('0.4');
    expect(strengthFor('abc', false, '0.7', '0.4')).toBe('0.4');
    expect(strengthFor('1.5', false, '0.7', '0.4')).toBe('0.4');
    expect(strengthFor('0', false, '0.7', '0.4')).toBe('0.4');
  });
});
