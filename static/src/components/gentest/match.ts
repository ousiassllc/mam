/**
 * The prompt match form of the generation test (#916): the reference image chosen (or dropped) is shown in the drop zone, the target
 * and the limit of rounds are shown only for the way that repeats on its own, and the slider writes its value beside it. The rules
 * are in match-model.ts. The listeners are on the document: the form may arrive in a swapped body.
 */

import { imageProblem, showsLimits, targetText } from './match-model';

const FORM = '[data-gt-match]';

function form(el: Element): HTMLElement | null {
  return el.closest<HTMLElement>(FORM);
}

function say(root: HTMLElement, text: string): void {
  const line = root.querySelector<HTMLElement>('[data-gt-match-error]');
  if (!line) return;
  line.textContent = text;
  line.hidden = text === '';
}

function show(root: HTMLElement, file: File): void {
  const preview = root.querySelector<HTMLImageElement>('[data-gt-match-preview]');
  const hint = root.querySelector<HTMLElement>('[data-gt-match-hint]');
  const ref = root.querySelector<HTMLInputElement>('[data-gt-match-ref]');
  if (preview) {
    if (preview.src.startsWith('blob:')) URL.revokeObjectURL(preview.src);
    preview.src = URL.createObjectURL(file);
    preview.hidden = false;
  }
  if (hint) hint.hidden = true;
  if (ref) ref.value = ''; // the file is the reference now
}

function onFile(input: HTMLInputElement, root: HTMLElement): void {
  const file = input.files?.[0];
  if (!file) return;
  const problem = imageProblem(file, Number(root.dataset.maxBytes ?? 0));
  say(root, problem);
  if (problem) {
    input.value = '';
    return;
  }
  show(root, file);
}

function onMode(input: HTMLInputElement, root: HTMLElement): void {
  const limits = root.querySelector<HTMLElement>('[data-gt-match-auto]');
  if (limits) limits.hidden = !showsLimits(input.value);
}

/** Installs the form's behaviour. Call it once (main.ts). */
export function initGentestMatch(root: Document = document): void {
  root.addEventListener('change', (event) => {
    const el = event.target as HTMLInputElement | null;
    const host = el && form(el);
    if (!el || !host) return;
    if (el.matches('[data-gt-match-file]')) onFile(el, host);
    if (el.matches('[data-gt-match-mode]') && el.checked) onMode(el, host);
  });
  root.addEventListener('input', (event) => {
    const el = event.target as HTMLInputElement | null;
    const host = el && form(el);
    if (!el || !host || !el.matches('[data-gt-match-target]')) return;
    const out = host.querySelector<HTMLElement>('[data-gt-match-target-output]');
    if (out) out.textContent = targetText(el.value);
  });
  // A file dropped on the zone is the chosen one; anywhere else the browser keeps its own behaviour.
  root.addEventListener('dragover', (event) => {
    if ((event.target as Element | null)?.closest('[data-gt-match-zone]')) event.preventDefault();
  });
  root.addEventListener('drop', (event) => {
    const zone = (event.target as Element | null)?.closest('[data-gt-match-zone]');
    const host = zone && form(zone);
    const file = event.dataTransfer?.files?.[0];
    const input = host?.querySelector<HTMLInputElement>('[data-gt-match-file]');
    if (!zone || !host || !input) return;
    event.preventDefault();
    if (!file) return;
    const files = new DataTransfer();
    files.items.add(file);
    input.files = files.files;
    input.dispatchEvent(new Event('change', { bubbles: true }));
  });
}
