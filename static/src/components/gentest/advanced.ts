/**
 * The "詳細設定" section of the generation test form (#973: seed, steps, cfg, sampler, scheduler). Each setting is shown while a ticked
 * model takes it (the cards say so: models.templ) and is disabled while it is not shown, so that it posts nothing; the steps and the cfg
 * are sliders (the common slider, #955) that are on only while their "上書きする" box is ticked. A note names the ticked models that do
 * not take a setting that was given. The rules are in advanced-model.ts. The listeners are on the document: the form may arrive in a
 * swapped body.
 */

import { KINDS, minStepsOf, note, shown, startValue, useOf, type Kind, type ModelUse } from './advanced-model';

const SECTION = '[data-gt-advanced]';

/** The models that are ticked in the form (one that the commercial filter hid is disabled and does not count). */
function chosen(form: HTMLElement): ModelUse[] {
  return [...form.querySelectorAll<HTMLInputElement>('[data-model] input[name="model"]:checked:not(:disabled)')].map(
    (input) => useOf(input.closest<HTMLElement>('[data-model]')!.dataset),
  );
}

function field(section: HTMLElement, kind: Kind): HTMLElement | null {
  return section.querySelector<HTMLElement>(`[data-gt-adv-field="${kind}"]`);
}

/** The control a setting posts with: the number input, the slider or the select. */
function control(el: HTMLElement): HTMLInputElement | HTMLSelectElement | null {
  return el.querySelector<HTMLInputElement | HTMLSelectElement>('input[name],select[name]');
}

function range(el: HTMLElement): HTMLInputElement | null {
  return el.querySelector<HTMLInputElement>('[data-gt-adv-range]');
}

function box(el: HTMLElement): HTMLInputElement | null {
  return el.querySelector<HTMLInputElement>('[data-gt-adv-on]');
}

/** Writes the value beside a slider: "既定" while it is off. */
function writeOutput(el: HTMLElement): void {
  const out = el.querySelector<HTMLElement>('[data-gt-adv-out]');
  const r = range(el);
  if (out && r) out.textContent = box(el)?.checked ? r.value : '既定';
}

/** Whether a setting posts: shown, and (for a slider) switched on. */
function posts(el: HTMLElement, show: boolean): boolean {
  return show && (box(el)?.checked ?? true);
}

/** Whether a setting is given to the models: it has a value that posts. */
function given(el: HTMLElement, show: boolean): boolean {
  const c = control(el);
  return posts(el, show) && !!c && c.value !== '';
}

/** The steps slider starts at the largest minimum of the chosen models (2 for a two-stage sampler), and a value below it moves up to it. */
function fitSteps(r: HTMLInputElement, models: readonly ModelUse[]): void {
  r.min = String(minStepsOf(models, 1));
  if (Number(r.value) < Number(r.min)) {
    r.value = r.min;
    r.dispatchEvent(new Event('input', { bubbles: true })); // the common slider repaints its fill
  }
}

function sync(section: HTMLElement): void {
  const form = section.closest<HTMLElement>('form') ?? section;
  const models = chosen(form);
  const asked: Kind[] = [];
  for (const kind of KINDS) {
    const el = field(section, kind);
    if (!el) continue;
    const show = shown(models, kind);
    el.hidden = !show;
    const c = control(el);
    const on = box(el);
    if (on) on.disabled = !show;
    const r = range(el);
    if (r) r.disabled = !posts(el, show);
    else if (c) c.disabled = !show;
    if (r && kind === 'steps') fitSteps(r, models);
    writeOutput(el);
    if (given(el, show)) asked.push(kind);
  }
  const line = section.querySelector<HTMLElement>('[data-gt-adv-note]');
  if (line) {
    line.textContent = note(models, asked);
    line.hidden = line.textContent === '';
  }
}

function syncAll(root: ParentNode = document): void {
  for (const section of root.querySelectorAll<HTMLElement>(SECTION)) sync(section);
}

function onSwitch(on: HTMLInputElement, section: HTMLElement): void {
  const el = on.closest<HTMLElement>('[data-gt-adv-field]');
  const r = el && range(el);
  const kind = el?.dataset.gtAdvField;
  if (!el || !r || (kind !== 'steps' && kind !== 'cfg')) return;
  if (on.checked) {
    const form = section.closest<HTMLElement>('form') ?? section;
    r.value = String(startValue(chosen(form), kind, Number(r.value)));
    r.dispatchEvent(new Event('input', { bubbles: true })); // the common slider repaints its fill
  }
  sync(section);
}

/** Installs the section's behaviour. Call it once (main.ts). */
export function initGentestAdvanced(root: Document = document): void {
  root.addEventListener('change', (event) => {
    const el = event.target as HTMLElement | null;
    const section = el?.closest<HTMLElement>(SECTION);
    if (el instanceof HTMLInputElement && section && el.matches('[data-gt-adv-on]')) return onSwitch(el, section);
    if (section) return sync(section);
    // a model ticked or the commercial filter moved: every section of the page follows
    if (el instanceof HTMLInputElement && el.name === 'model') syncAll(el.closest('form') ?? root);
  });
  root.addEventListener('input', (event) => {
    const el = event.target as HTMLElement | null;
    const section = el?.closest<HTMLElement>(SECTION);
    if (!section || !el?.matches('[data-gt-adv-range]')) return;
    writeOutput(el.closest<HTMLElement>('[data-gt-adv-field]')!);
  });
  root.addEventListener('commercial-filter-applied', () => syncAll(root));
  root.addEventListener('htmx:load', (event) => syncAll(event.target as ParentNode));
  root.addEventListener('DOMContentLoaded', () => syncAll(root));
}
