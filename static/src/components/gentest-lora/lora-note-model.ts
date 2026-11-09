// What the form of the generation test says about the LoRA that is chosen (#1119) without the DOM: the base model it is made for against the models that
// are ticked, the recommended strength, the trigger words and the license. lora-note.ts reads the facts from the data attributes the server wrote and draws
// the lines. Nothing is guessed: what the file does not say is "未設定", and a license nobody entered is "不明".

export type Commercial = 'yes' | 'conditional' | 'no' | 'unknown';

/** What the model manager knows of a LoRA file (the data attributes of its <option>). */
export interface LoRAFacts {
  /** False for a file the model manager did not add (put in the folder by hand, or a catalog model's). */
  known: boolean;
  /** lorafamily keys; none: not set. */
  families: string[];
  /** The words of the families, for the sentence. */
  familyLabels: string;
  /** The recommended strength as the server wrote it ("" when not set). */
  strength: string;
  triggers: string[];
  commercial: Commercial;
}

/** A ticked model that takes a LoRA: its name and the family of base model its workflow is ("" when it does not say). */
export interface ModelFamily {
  label: string;
  family: string;
  familyLabel: string;
}

export interface NoteLine {
  tone: 'info' | 'warn';
  text: string;
}

const COMMERCIAL_TEXT: Record<Commercial, string> = {
  yes: '商用OK',
  conditional: '条件つき（利用者の自己申告）',
  no: '商用NG',
  unknown: '不明',
};

/** The models whose family the LoRA does not list: both are known and they differ (a LoRA or a model without a family is not a mismatch). */
export function mismatches(l: LoRAFacts, models: ModelFamily[]): ModelFamily[] {
  if (l.families.length === 0) return [];
  return models.filter((m) => m.family !== '' && !l.families.includes(m.family));
}

/** The lines of the note under the LoRA select. `lora` is null while "なし" is chosen: no note. */
export function loraNote(lora: LoRAFacts | null, models: ModelFamily[], commercialOnly: boolean): NoteLine[] {
  if (!lora) return [];
  if (!lora.known) {
    return [
      {
        tone: 'info',
        text: 'モデルマネージャーから追加したファイルではないため、対応するベースモデル・推奨の強さ・トリガー語・ライセンスは未設定です。',
      },
    ];
  }
  const lines: NoteLine[] = [];
  lines.push({
    tone: 'info',
    text: `対応するベースモデル: ${lora.families.length > 0 ? lora.familyLabels : '未設定'}`,
  });
  for (const m of mismatches(lora, models)) {
    lines.push({
      tone: 'warn',
      text: `「${m.label}」は ${m.familyLabel} 用のモデルです。この LoRA は ${lora.familyLabels} 用のため、合わない組み合わせです（効かない・画が崩れることがあります）。`,
    });
  }
  if (lora.families.length === 0 && models.some((m) => m.family !== '')) {
    lines.push({ tone: 'warn', text: 'ベースモデルが未設定のため、選んだモデルに合うか確かめられません。' });
  }
  lines.push({ tone: 'info', text: `推奨の強さ: ${lora.strength !== '' ? lora.strength : '未設定'}` });
  lines.push({
    tone: 'info',
    text: `トリガー語: ${lora.triggers.length > 0 ? `${lora.triggers.join('、')}（プロンプトに入れてください）` : '未設定'}`,
  });
  lines.push({ tone: 'info', text: `商用利用: ${COMMERCIAL_TEXT[lora.commercial]}` });
  if (commercialOnly && lora.commercial !== 'yes') {
    lines.push({
      tone: 'warn',
      text: '「商用OKのモデルだけを使う」がオンですが、この LoRA は商用OKと確認できていません。生成は止めません。',
    });
  }
  return lines;
}

/** The commercial answer a data attribute says; anything else is unknown (never 商用OK by default). */
export function commercialOf(value: string | undefined): Commercial {
  return value === 'yes' || value === 'conditional' || value === 'no' ? value : 'unknown';
}

/** The strength a LoRA is chosen with: the recommended one when it is set and the user has not moved the slider, else the form's default. */
export function strengthFor(recommended: string, touched: boolean, current: string, fallback: string): string {
  if (touched) return current;
  const n = Number(recommended);
  return recommended !== '' && Number.isFinite(n) && n > 0 && n <= 1 ? String(n) : fallback;
}
