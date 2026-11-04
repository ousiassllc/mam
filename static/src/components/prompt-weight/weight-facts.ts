/**
 * How the chosen models read the weights of a prompt (Issue #972, #977, #1023): the cards carry it (data-prompt-weight: supported / weak /
 * unsupported / unknown, and data-prompt-weight-title: the short words that say it, "効きます" / "効きが弱いかも" / "効きません（…）" / "未確認";
 * backend/.../gentestadv, settingsview.WeightTitle). The "強める + / 弱める −" buttons work while some chosen model reads the weights; when every
 * one of them does not, they are off and the note says why. Models that read them the same way are said once, by their display names (the card's
 * name, never an identifier): the "?" of the prompts and the note beside the prompt both read the chosen models this way.
 */

export type Level = 'supported' | 'weak' | 'unsupported' | 'unknown';

/** One chosen model's way of reading weights. */
export interface ModelWeight {
  label: string;
  level: Level;
  title: string;
}

/** The chosen models that read the weights the same way. */
export interface Group {
  level: Level;
  title: string;
  labels: string[];
}

/** What the chosen models say as a whole. */
export interface Facts {
  /** False when the models are chosen and none of them reads the weights. */
  usable: boolean;
  /** The groups, what works first (supported, weak, unsupported, unknown); empty while no model is chosen. */
  groups: Group[];
}

/** No model chosen: nothing is known to be refused, and nothing is said. */
export const NO_FACTS: Facts = { usable: true, groups: [] };

const ORDER: readonly Level[] = ['supported', 'weak', 'unsupported', 'unknown'];

/** The short words of a level, for a card that does not carry them. */
const TITLES: Record<Level, string> = {
  supported: '効きます',
  weak: '効きが弱いかも',
  unsupported: '効きません（代わりに文で強調してください）',
  unknown: '未確認',
};

function level(value: string | undefined): Level {
  return value === 'supported' || value === 'weak' || value === 'unsupported' ? value : 'unknown';
}

/** Reads a model card's data attributes. The name is the card's own (data-label); the identifier is only the last resort of a card without one. */
export function modelWeightOf(data: DOMStringMap): ModelWeight {
  const l = level(data.promptWeight);
  return { label: data.label ?? data.model ?? '', level: l, title: data.promptWeightTitle || TITLES[l] };
}

/** The chosen models by how they read the weights: one group for each way, each name once. */
export function factsOf(models: readonly ModelWeight[]): Facts {
  if (models.length === 0) return NO_FACTS;
  const groups: Group[] = [];
  for (const l of ORDER) {
    const same = models.filter((m) => m.level === l);
    const first = same[0];
    if (first) groups.push({ level: l, title: first.title, labels: [...new Set(same.map((m) => m.label))] });
  }
  return { usable: models.some((m) => m.level !== 'unsupported'), groups };
}

/** "名前・名前: 効きます" — the line of a group (the same in the "?" and in the note). */
export function lineOf(group: Group): string {
  return `${group.labels.join('・')}: ${group.title}`;
}

/** The short caution for the models that do not read the weights, "" when every chosen model does (or none is chosen). */
export function cautionOf(facts: Facts): string {
  const group = facts.groups.find((g) => g.level === 'unsupported');
  if (!group) return '';
  return `${group.labels.join('・')} では、ウェイト (語:1.3) は効きません。強調したい語は、文で言い換えてください（「特に」「とても」を付ける、先頭に置く、など）`;
}

/** What the bar of the selected words says under its buttons: the caution for the models that do not read the weights, and the line of the ones that read them poorly. */
export function notesOf(facts: Facts): string[] {
  const weak = facts.groups.find((g) => g.level === 'weak');
  return [cautionOf(facts), weak ? lineOf(weak) : ''].filter((n) => n !== '');
}
