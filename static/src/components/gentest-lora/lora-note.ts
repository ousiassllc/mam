/**
 * The note under the LoRA choice of the generation test (#1119): what is known of the chosen LoRA, a warning when its base model does not fit a
 * ticked model, and the recommended strength put on the slider until the user moves it. The rules are in lora-note-model.ts. The listeners are on the
 * document: the form may arrive in a swapped body.
 */

import { syncSliders } from '../slider/slider';
import {
  commercialOf,
  loraNote,
  strengthFor,
  type LoRAFacts,
  type ModelFamily,
  type NoteLine,
} from './lora-note-model';

const SELECT = '#gt-lora-name';
const NOTE = '[data-gt-lora-note]';

function split(value: string | undefined, sep: string): string[] {
  return (value ?? '')
    .split(sep)
    .map((s) => s.trim())
    .filter(Boolean);
}

function factsOf(select: HTMLSelectElement): LoRAFacts | null {
  const option = select.selectedOptions[0];
  if (!option || option.value === '') return null;
  const d = option.dataset;
  return {
    known: d.known === 'true',
    families: split(d.families, ' '),
    familyLabels: d.familyLabels ?? '',
    strength: d.strength ?? '',
    triggers: split(d.triggers, ','),
    commercial: commercialOf(d.commercial),
  };
}

/** The ticked models that take a LoRA. */
function modelsOf(form: ParentNode): ModelFamily[] {
  const out: ModelFamily[] = [];
  for (const input of form.querySelectorAll<HTMLInputElement>(
    '[data-model] input[name="model"]:checked:not(:disabled)',
  )) {
    const card = input.closest<HTMLElement>('[data-model]');
    if (card?.dataset.supportsLora !== 'true') continue;
    out.push({
      label: card.dataset.label ?? card.dataset.model ?? '',
      family: card.dataset.loraFamily ?? '',
      familyLabel: card.dataset.loraFamilyLabel ?? '',
    });
  }
  return out;
}

function draw(note: HTMLElement, lines: NoteLine[]): void {
  note.replaceChildren(
    ...lines.map((line) => {
      const li = document.createElement('li');
      li.textContent = (line.tone === 'warn' ? '⚠ ' : '') + line.text;
      li.className = line.tone === 'warn' ? 'text-warning' : 'text-mute';
      if (line.tone === 'warn') li.setAttribute('role', 'alert');
      return li;
    }),
  );
  note.hidden = lines.length === 0;
}

/** Draws the note of the form that holds `select`. */
export function refreshLoRANote(select: HTMLSelectElement): void {
  const scope = select.closest('form') ?? document;
  const note = scope.querySelector<HTMLElement>(NOTE);
  if (!note) return;
  draw(note, loraNote(factsOf(select), modelsOf(scope), note.hasAttribute('data-commercial-only')));
}

/** Puts the recommended strength of the chosen LoRA on its slider while the user has not moved it. */
function applyStrength(select: HTMLSelectElement): void {
  const slider = select.closest('[data-gt-lora]')?.querySelector<HTMLInputElement>('[data-gt-lora-strength]');
  if (!slider) return;
  const recommended = select.selectedOptions[0]?.dataset.strength ?? '';
  const next = strengthFor(recommended, slider.dataset.touched !== undefined, slider.value, slider.defaultValue);
  if (next === slider.value) return;
  slider.value = next;
  const out = slider.closest('label')?.querySelector('output');
  if (out) out.textContent = Number(next).toFixed(2);
  syncSliders(slider);
}

function refreshAll(root: ParentNode): void {
  for (const select of root.querySelectorAll<HTMLSelectElement>(SELECT)) refreshLoRANote(select);
}

/** Installs the note. Call it once (main.ts). */
export function initGentestLoRA(root: Document = document): void {
  root.addEventListener('change', (event) => {
    const el = event.target as HTMLElement | null;
    if (!el) return;
    if (el.matches(SELECT)) {
      applyStrength(el as HTMLSelectElement);
      refreshLoRANote(el as HTMLSelectElement);
    } else if (el.matches('input[name="model"]')) {
      refreshAll(el.closest('form') ?? root);
    }
  });
  root.addEventListener('htmx:load', (event) => refreshAll((event.target as ParentNode | null) ?? root));
  root.addEventListener('DOMContentLoaded', () => refreshAll(root));
}
