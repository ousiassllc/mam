/**
 * The note under the LoRA choice of the generation test (#1119): what is known of the chosen LoRA, a warning when its base model does not fit a
 * ticked model, and the recommended strength put on the slider until the user moves it. The rules are in lora-note-model.ts. The choice is the field of the
 * model picker (#1011, backend/internal/web/molecules/modelpick): what is known of the file is in its hidden inputs (data-mp-from), which the card that is chosen
 * sets. The listeners are on the document: the form may arrive in a swapped body.
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

const FIELD = '#gt-lora-name';
const NOTE = '[data-gt-lora-note]';

function split(value: string | undefined, sep: string): string[] {
  return (value ?? '')
    .split(sep)
    .map((s) => s.trim())
    .filter(Boolean);
}

/** What the hidden inputs of the field hold of the chosen file, by the key of the card (modelcards.LoRAKey*). */
function valuesOf(field: HTMLElement): Record<string, string> {
  const out: Record<string, string> = {};
  for (const input of field.querySelectorAll<HTMLInputElement>('[data-mp-from]'))
    out[input.dataset.mpFrom ?? ''] = input.value;
  return out;
}

function factsOf(field: HTMLElement): LoRAFacts | null {
  const v = valuesOf(field);
  if (!v.value) return null;
  return {
    known: v.known === 'true',
    families: split(v.families, ' '),
    familyLabels: v.family_labels ?? '',
    strength: v.strength ?? '',
    triggers: split(v.triggers, ','),
    commercial: commercialOf(v.commercial),
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

/** Draws the note of the form that holds the LoRA `field`. */
export function refreshLoRANote(field: HTMLElement): void {
  const scope = field.closest('form') ?? document;
  const note = scope.querySelector<HTMLElement>(NOTE);
  if (!note) return;
  draw(note, loraNote(factsOf(field), modelsOf(scope), note.hasAttribute('data-commercial-only')));
}

/** Puts the recommended strength of the chosen LoRA on its slider while the user has not moved it. */
function applyStrength(field: HTMLElement): void {
  const slider = field.closest('[data-gt-lora]')?.querySelector<HTMLInputElement>('[data-gt-lora-strength]');
  if (!slider) return;
  const recommended = valuesOf(field).strength ?? '';
  const next = strengthFor(recommended, slider.dataset.touched !== undefined, slider.value, slider.defaultValue);
  if (next === slider.value) return;
  slider.value = next;
  const out = slider.closest('label')?.querySelector('output');
  if (out) out.textContent = Number(next).toFixed(2);
  syncSliders(slider);
}

function refreshAll(root: ParentNode): void {
  for (const field of root.querySelectorAll<HTMLElement>(FIELD)) refreshLoRANote(field);
}

/** Installs the note. Call it once (main.ts). */
export function initGentestLoRA(root: Document = document): void {
  root.addEventListener('change', (event) => {
    const el = event.target as HTMLElement | null;
    if (!el) return;
    const field = el.matches('[data-mp-from]') ? el.closest<HTMLElement>(FIELD) : null;
    if (field) {
      applyStrength(field);
      refreshLoRANote(field);
    } else if (el.matches('input[name="model"]')) {
      refreshAll(el.closest('form') ?? root);
    }
  });
  root.addEventListener('htmx:load', (event) => refreshAll((event.target as ParentNode | null) ?? root));
  root.addEventListener('DOMContentLoaded', () => refreshAll(root));
}
