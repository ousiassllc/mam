// The rules of the "詳細設定" section of the generation test form (Issue #973): which settings the chosen models take, which of them are
// shown, the default a slider starts from and the note for the models that do not take what was set. The cards carry the facts
// (data-sampling, data-supports-seed, data-sampling-locked, data-default-steps, data-min-steps, data-default-cfg; models.templ).

/** The settings the section draws, in their order. */
export const KINDS = ['seed', 'steps', 'cfg', 'sampler', 'scheduler'] as const;
export type Kind = (typeof KINDS)[number];

const NAMES: Record<Kind, string> = {
  seed: 'シード',
  steps: 'ステップ数',
  cfg: 'CFG',
  sampler: 'サンプラー',
  scheduler: 'スケジューラー',
};

/** What one ticked model takes of the settings. */
export interface ModelUse {
  label: string;
  takes: ReadonlySet<Kind>;
  /** The model's speed mode owns the steps and the cfg: they are not taken, and the note says why. */
  locked: boolean;
  defaultSteps: number;
  /** The fewest steps the model takes (2 for a two-stage sampler such as Wan 2.2: one step leaves its second stage nothing to do). */
  minSteps: number;
  defaultCfg: number;
}

/** Reads the data attributes of a model card. */
export function useOf(data: DOMStringMap): ModelUse {
  const takes = new Set<Kind>(
    (data.sampling ?? '').split(' ').filter((n): n is Kind => (KINDS as readonly string[]).includes(n)),
  );
  if (data.supportsSeed === 'true') takes.add('seed');
  return {
    label: data.label ?? data.model ?? '',
    takes,
    locked: data.samplingLocked === 'true',
    defaultSteps: Number(data.defaultSteps),
    minSteps: Number(data.minSteps) || 1,
    defaultCfg: Number(data.defaultCfg),
  };
}

/** A setting is shown while a chosen model takes it; with no model chosen every setting is (nothing is known to be refused). */
export function shown(models: readonly ModelUse[], kind: Kind): boolean {
  return models.length === 0 || models.some((m) => m.takes.has(kind));
}

/** The value a slider starts from when it is switched on: the default of the first chosen model that takes it; `fallback` if none does. */
export function startValue(models: readonly ModelUse[], kind: 'steps' | 'cfg', fallback: number): number {
  const m = models.find((x) => x.takes.has(kind));
  const v = m ? (kind === 'steps' ? m.defaultSteps : m.defaultCfg) : NaN;
  return Number.isFinite(v) ? v : fallback;
}

/** The lowest the steps slider goes: the largest minimum of the chosen models that take the steps (1 if none does), so that one value is right for all of them. */
export function minStepsOf(models: readonly ModelUse[], fallback: number): number {
  return models.filter((m) => m.takes.has('steps')).reduce((n, m) => Math.max(n, m.minSteps), fallback);
}

/** The note for the chosen models that do not take a setting that was given ("" when every one takes all of them). */
export function note(models: readonly ModelUse[], given: readonly Kind[]): string {
  const lines: string[] = [];
  for (const m of models) {
    const lost = given.filter((k) => !m.takes.has(k));
    const owned: Kind[] = m.locked ? lost.filter((k) => k === 'steps' || k === 'cfg') : [];
    const rest = lost.filter((k) => !owned.includes(k));
    if (owned.length > 0) lines.push(`${m.label}: ${names(owned)} は速さのモードが決めるため、指定は使いません`);
    if (rest.length > 0) lines.push(`${m.label}: ${names(rest)} は受け取らないため、指定は使いません`);
  }
  return lines.join('\n');
}

function names(kinds: readonly Kind[]): string {
  return kinds.map((k) => NAMES[k]).join('・');
}
