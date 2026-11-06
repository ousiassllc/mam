// The rules of the "GPT Image 2.6 の設定" section of the generation test form (Issue #1096), without the DOM: when the section is in view, which of its
// fields are posted, and what it says about the ticked models that cannot take the settings. The model cards carry the facts (data-gpt-image,
// data-gpt-refusal; organisms/gentest/models_card.go). The words are those of handler/gentest/gptform (OtherNote), which draws the section first.

/** What one ticked model says of the settings: whether it takes them, and why it does not when it is of the family and refused them. */
export interface ModelGpt {
  label: string;
  takes: boolean;
  refusal: string;
}

/** Reads the data attributes of a model card. */
export function gptOf(data: DOMStringMap): ModelGpt {
  return { label: data.label ?? data.model ?? '', takes: data.gptImage === 'true', refusal: data.gptRefusal ?? '' };
}

/** What the section shows: itself, its fields (posted), and the lines about the models that cannot take what is set. */
export interface GptView {
  shown: boolean;
  fields: boolean;
  notes: string[];
}

/**
 * The section is in view while a ticked model takes the settings or is refused them (the reason is its note), or a value is given. Its fields are posted while a
 * ticked model takes them or a value is given: a value that a model cannot take stays in the form and posts, so that the service refuses it with the reason
 * and it is never dropped quietly. `given` says the complexity or the output format has a value.
 */
export function viewOf(models: readonly ModelGpt[], given: boolean): GptView {
  const takes = models.some((m) => m.takes);
  const notes: string[] = [];
  for (const m of models) {
    if (m.takes) continue;
    if (m.refusal !== '') notes.push(`${m.label}: ${m.refusal}`);
    else if (given) notes.push(otherNote(m.label));
  }
  const refused = models.some((m) => m.refusal !== '');
  return { shown: takes || refused || given, fields: takes || given, notes };
}

/** The note for a ticked model that is not of the family while a setting is given. */
export function otherNote(label: string): string {
  return `${label}: GPT Image 2.6 の設定（複雑さ・出力形式）は受け取りません。指定なしに戻すか、このモデルを外してください`;
}

/** The frames of a GIF are asked for only while the output format is GIF. */
export function framesShown(format: string): boolean {
  return format === 'gif';
}
