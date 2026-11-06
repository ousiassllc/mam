/**
 * The "GPT Image 2.6 の設定" section of the generation test form (#1096: complexity, output format, the frames of a GIF). It is in view while a ticked model
 * takes the settings (GPT Image 2.6 Flare / Sunburst through the API key) or is refused them (their ChatGPT-subscription twins: the reason is written under
 * it); a field that is not in view is disabled, so that it posts nothing, and the frames are asked for only for a GIF. The rules are in gpt-image-model.ts.
 */

import { framesShown, gptOf, viewOf, type ModelGpt } from './gpt-image-model';

const SECTION = '[data-gt-gpt]';

/** The models that are ticked in the form (one that the commercial filter hid is disabled and does not count). */
function chosen(form: HTMLElement): ModelGpt[] {
  return [...form.querySelectorAll<HTMLInputElement>('[data-model] input[name="model"]:checked:not(:disabled)')].map(
    (input) => gptOf(input.closest<HTMLElement>('[data-model]')!.dataset),
  );
}

function sync(section: HTMLElement): void {
  const form = section.closest<HTMLElement>('form') ?? section;
  const complexity = section.querySelector<HTMLSelectElement>('[data-testid="gt-gpt-complexity"]');
  const format = section.querySelector<HTMLSelectElement>('[data-gt-gpt-format]');
  const given = (complexity?.value ?? '') !== '' || (format?.value ?? '') !== '';
  const view = viewOf(chosen(form), given);
  const gif = framesShown(format?.value ?? '');
  section.hidden = !view.shown;
  const fields = section.querySelector<HTMLElement>('[data-gt-gpt-fields]');
  if (fields) fields.hidden = !view.fields;
  const frames = section.querySelector<HTMLElement>('[data-gt-gpt-frames]');
  if (frames) frames.hidden = !gif;
  for (const control of section.querySelectorAll<HTMLInputElement | HTMLSelectElement>('[data-gt-gpt-control]')) {
    const isFrame = frames?.contains(control) ?? false;
    control.disabled = !view.fields || (isFrame && !gif);
  }
  for (const hint of section.querySelectorAll<HTMLElement>('[data-gt-gpt-hint]')) {
    hint.hidden = hint.dataset.gtGptHint !== (format?.value ?? '');
  }
  const notes = section.querySelector<HTMLElement>('[data-gt-gpt-notes]');
  if (notes) {
    notes.replaceChildren(
      ...view.notes.map((text) => {
        const li = document.createElement('li');
        li.className = 'break-words';
        li.textContent = text;
        return li;
      }),
    );
    notes.hidden = view.notes.length === 0;
  }
}

function syncAll(root: ParentNode = document): void {
  for (const section of root.querySelectorAll<HTMLElement>(SECTION)) sync(section);
}

/** Installs the section's behaviour. Call it once (main.ts). */
export function initGentestGptImage(root: Document = document): void {
  root.addEventListener('change', (event) => {
    const el = event.target as HTMLElement | null;
    const section = el?.closest<HTMLElement>(SECTION);
    if (section) return sync(section);
    // a model ticked or the commercial filter moved: every section of the form follows
    if (el instanceof HTMLInputElement && el.name === 'model') syncAll(el.closest('form') ?? root);
  });
  root.addEventListener('model-pick-applied', () => syncAll(root));
  root.addEventListener('commercial-filter-applied', () => syncAll(root));
  root.addEventListener('htmx:load', (event) => syncAll(event.target as ParentNode));
  root.addEventListener('DOMContentLoaded', () => syncAll(root));
}
