/**
 * How the chosen models read the weights of a prompt (Issue #972, #977): the cards carry it (data-prompt-weight: supported / weak /
 * unsupported / unknown, and data-prompt-weight-text: the sentence that says it; backend/.../gentestadv, settingsview.WeightText).
 * The "強める + / 弱める −" buttons work while some chosen model reads the weights; when every one of them does not, they are off and
 * the note says why. A weak or unknown model keeps them on with its note.
 */

export type Level = 'supported' | 'weak' | 'unsupported' | 'unknown';

/** One chosen model's way of reading weights. */
export interface ModelWeight {
  label: string;
  level: Level;
  text: string;
}

/** What the chosen models say as a whole. */
export interface Facts {
  /** False when the models are chosen and none of them reads the weights. */
  usable: boolean;
  /** One line for each model that does not read them for sure (named when there are several models). */
  notes: string[];
}

/** No model chosen: nothing is known to be refused, and nothing is said. */
export const NO_FACTS: Facts = { usable: true, notes: [] };

function level(value: string | undefined): Level {
  return value === 'supported' || value === 'weak' || value === 'unsupported' ? value : 'unknown';
}

/** Reads a model card's data attributes. */
export function modelWeightOf(data: DOMStringMap): ModelWeight {
  return { label: data.label ?? data.model ?? '', level: level(data.promptWeight), text: data.promptWeightText ?? '' };
}

/** What the weights come to for the chosen models. */
export function factsOf(models: readonly ModelWeight[]): Facts {
  if (models.length === 0) return NO_FACTS;
  const notes = models
    .filter((m) => m.level !== 'supported' && m.text !== '')
    .map((m) => (models.length > 1 ? `${m.label}: ${m.text}` : m.text));
  return { usable: models.some((m) => m.level !== 'unsupported'), notes };
}
