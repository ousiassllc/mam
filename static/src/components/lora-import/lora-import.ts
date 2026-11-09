/**
 * The "ファイルを追加" form of the model manager (#1119): a file chosen with the button or dropped on the zone is checked here (the extension and the size; the
 * server checks it all again) and sent by htmx, with the progress of the transfer under the zone. The rules are in lora-import-model.ts. The listeners are on
 * the document: the section may arrive in a swapped body.
 */

import { fileProblem, progressText } from './lora-import-model';

const FORM = '[data-lora-pick]';
const ZONE_ACTIVE = ['border-accent', 'bg-accent-soft'];

function say(form: HTMLElement, text: string): void {
  const line = form.querySelector<HTMLElement>('[data-lora-error]');
  if (!line) return;
  line.textContent = text;
  line.hidden = text === '';
}

function maxBytes(form: HTMLElement): number {
  return Number(form.closest<HTMLElement>('[data-max-bytes]')?.dataset.maxBytes ?? 0);
}

function showProgress(form: HTMLElement, loaded: number, total: number): void {
  const box = form.querySelector<HTMLElement>('[data-lora-progress]');
  if (!box) return;
  box.hidden = false;
  const bar = box.querySelector<HTMLProgressElement>('progress');
  if (bar) bar.value = total > 0 ? Math.min(100, (loaded / total) * 100) : 0;
  const text = box.querySelector<HTMLElement>('[data-lora-progress-text]');
  if (text) text.textContent = progressText(loaded, total);
}

/** Checks the file and sends the form (htmx handles the submit). */
function send(form: HTMLFormElement, input: HTMLInputElement): void {
  const file = input.files?.[0];
  if (!file) return;
  const problem = fileProblem(file, maxBytes(form));
  say(form, problem);
  if (problem) {
    input.value = '';
    return;
  }
  showProgress(form, 0, file.size);
  form.requestSubmit();
}

/** Installs the form's behaviour. Call it once (main.ts). */
export function initLoRAImport(root: Document = document): void {
  root.addEventListener('change', (event) => {
    const input = event.target as HTMLInputElement | null;
    const form = input?.closest<HTMLFormElement>(FORM);
    if (input && form && input.matches('[data-lora-file]')) send(form, input);
  });
  // A file dropped on the zone is the chosen one; anywhere else the browser keeps its own behaviour.
  root.addEventListener('dragover', (event) => {
    const zone = (event.target as Element | null)?.closest('[data-lora-zone]');
    if (!zone) return;
    event.preventDefault();
    zone.classList.add(...ZONE_ACTIVE);
  });
  root.addEventListener('dragleave', (event) => {
    (event.target as Element | null)?.closest('[data-lora-zone]')?.classList.remove(...ZONE_ACTIVE);
  });
  root.addEventListener('drop', (event) => {
    const zone = (event.target as Element | null)?.closest('[data-lora-zone]');
    const form = zone?.closest<HTMLFormElement>(FORM);
    const input = form?.querySelector<HTMLInputElement>('[data-lora-file]');
    if (!zone || !form || !input) return;
    event.preventDefault();
    if (form.classList.contains('htmx-request')) return; // one file is on its way: a second is not sent beside it
    zone.classList.remove(...ZONE_ACTIVE);
    const file = event.dataTransfer?.files?.[0];
    if (!file) return;
    const files = new DataTransfer();
    files.items.add(file);
    input.files = files.files;
    send(form, input);
  });
  // The progress of the transfer (htmx fires it on the element that sent the request).
  root.addEventListener('htmx:xhr:progress', (event) => {
    const form = (event.target as Element | null)?.closest<HTMLFormElement>(FORM);
    const detail = (event as CustomEvent<{ loaded: number; total: number; lengthComputable: boolean }>).detail;
    if (form && detail.lengthComputable) showProgress(form, detail.loaded, detail.total);
  });
}
