import { highlight, markAdded, releasePicker, RowReorder } from './inputs-dom';
import { applyModels } from './inputs-models';
import { isUploadRef, moveItem, takeFiles, uploadRef } from './inputs-model';

/**
 * The input images of the generation test (spec/components/gentest-screen.md): one drop zone (drop or click, plus a dialog that
 * lists the images of a Project or of the history, which HTMX fills), the chosen images as numbered thumbnails that can be
 * removed, reordered (drag, or the ‹ › buttons), enlarged (a dialog), and given a role and a strength.
 *
 * The images chosen in the browser are not uploaded until the estimate: each is a File that this keeps, shows through an object
 * URL, and puts into the input `upload` in the order of the thumbnails; its thumbnail posts `ref=upload::#<n>` for the n-th file
 * of that input, with `role_upload::#<n>` and `strength_upload::#<n>` (the server puts the staged file in its place).
 */

const inited = new WeakSet<HTMLElement>();

// The drop highlight of the zone: classes of the colour table (spec/components/color-rules.md), swapped in by the script.
const ZONE_REST = 'border-neutral-600';
const ZONE_ACTIVE = ['border-indigo-400', 'bg-indigo-500/20'];

class Inputs {
  private readonly form: HTMLElement;
  private readonly list: HTMLElement;
  private readonly fileInput: HTMLInputElement;
  private readonly files = new Map<HTMLElement, { file: File; url: string }>();
  private readonly max: number;
  private readonly maxBytes: number;
  // The listeners put on the form, which can outlive this (a form that is not swapped as a whole): removed with it.
  private readonly onForm = new AbortController();

  constructor(private readonly root: HTMLElement) {
    this.form = root.closest<HTMLElement>('form') ?? root;
    this.list = this.q('[data-gt-list]');
    this.fileInput = this.q<HTMLInputElement>('[data-gt-file]');
    this.max = Number(root.dataset.max) || 10;
    this.maxBytes = Number(root.dataset.maxBytes) || 0;
    this.listen();
    this.sync();
  }

  private q<T extends HTMLElement = HTMLElement>(selector: string): T {
    return this.root.querySelector<T>(selector)!;
  }

  private rows(): HTMLElement[] {
    return [...this.list.querySelectorAll<HTMLElement>(':scope > [data-gt-ref]')];
  }

  private listen(): void {
    const zone = this.q('[data-gt-dropzone]');
    this.fileInput.addEventListener('change', () =>
      this.add([...(this.fileInput.files ?? [])].filter((f) => !this.isKept(f))),
    );
    zone.addEventListener('click', (e) => {
      if (!(e.target as Element).closest('button,label,input')) this.fileInput.click();
    });
    zone.addEventListener('dragover', (e) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      highlight(zone, true, ZONE_REST, ZONE_ACTIVE);
    });
    zone.addEventListener('dragleave', (e) => {
      // Moving from the zone onto something inside it is not leaving it (the highlight would flicker).
      if (e.relatedTarget instanceof Node && zone.contains(e.relatedTarget)) return;
      highlight(zone, false, ZONE_REST, ZONE_ACTIVE);
    });
    zone.addEventListener('drop', (e) => {
      highlight(zone, false, ZONE_REST, ZONE_ACTIVE);
      if (!hasFiles(e)) return;
      e.preventDefault();
      this.add([...(e.dataTransfer?.files ?? [])]);
    });
    // An image dropped anywhere else on the form must not make the browser open it (the prompts would be lost): a drop that the
    // zone did not take is taken here, and added when it is on the thumbnail grid, else ignored.
    const { signal } = this.onForm;
    this.form.addEventListener('dragover', (e) => hasFiles(e) && e.preventDefault(), { signal });
    this.form.addEventListener(
      'drop',
      (e) => {
        if (!hasFiles(e) || e.defaultPrevented) return;
        e.preventDefault();
        if (e.target instanceof Node && this.list.contains(e.target)) this.add([...(e.dataTransfer?.files ?? [])]);
      },
      { signal },
    );
    this.root.addEventListener('click', (e) => this.onClick(e.target as Element));
    this.root.addEventListener('input', (e) => {
      const el = e.target as HTMLInputElement;
      if (el.type === 'range') {
        const out = el.closest('label')?.querySelector('output');
        if (out) out.textContent = Number(el.value).toFixed(2);
      }
    });
    new RowReorder(
      this.list,
      () => this.rows(),
      () => this.sync(),
    );
    new MutationObserver(() => this.sync()).observe(this.list, { childList: true });
    // The ticked models decide which controls are drawn.
    this.form.addEventListener(
      'change',
      (e) => {
        if ((e.target as HTMLInputElement).name === 'model') this.sync();
      },
      { signal },
    );
    this.q('[data-gt-picker-dialog]').addEventListener('htmx:afterRequest', markAdded);
    // The form is swapped as a whole (another media): its files go with it, so their object URLs are let go of.
    this.root.addEventListener('htmx:beforeCleanupElement', (e) => {
      if (e.target === this.root) this.dispose();
    });
  }

  private isKept(file: File): boolean {
    return [...this.files.values()].some((k) => k.file === file);
  }

  private onClick(el: Element): void {
    const row = el.closest<HTMLElement>('[data-gt-ref]');
    const picker = this.q<HTMLDialogElement>('[data-gt-picker-dialog]');
    if (el.closest('[data-gt-open-picker]')) picker.showModal();
    else if (el.closest('[data-gt-picker-close]')) picker.close();
    else if (el.closest('[data-gt-lightbox-close]')) this.q<HTMLDialogElement>('[data-gt-lightbox]').close();
    else if (el === this.q('[data-gt-lightbox]')) this.q<HTMLDialogElement>('[data-gt-lightbox]').close();
    else if (row && el.closest('[data-gt-remove]')) this.remove(row);
    else if (row && el.closest('[data-gt-move]'))
      this.move(row, Number(el.closest<HTMLElement>('[data-gt-move]')!.dataset.gtMove), el);
    else if (row && el.closest('[data-gt-zoom]')) this.zoom(row);
  }

  /** Adds files chosen or dropped: the ones the rules take, with one line about what was left out. */
  private add(chosen: File[]): void {
    const taken = takeFiles(chosen, this.max - this.rows().length, this.maxBytes, this.max);
    this.say(taken.error);
    const template = this.q<HTMLTemplateElement>('[data-gt-template]');
    for (const file of taken.files) {
      const li = template.content.firstElementChild!.cloneNode(true) as HTMLElement;
      const url = URL.createObjectURL(file);
      li.querySelector('img')!.src = url;
      for (const el of li.querySelectorAll<HTMLElement>('[aria-label],[title]')) {
        for (const attr of ['aria-label', 'title']) {
          const v = el.getAttribute(attr);
          if (v?.includes('__NAME__'))
            el.setAttribute(
              attr,
              v.replaceAll('__NAME__', () => file.name),
            );
        }
      }
      this.files.set(li, { file, url });
      this.list.append(li);
    }
    this.sync();
  }

  private remove(row: HTMLElement): void {
    const index = this.rows().indexOf(row);
    this.say('');
    this.forget(row);
    row.remove();
    this.sync();
    // The focus stays in the grid (on the thumbnail that took its place, else the one before it), or goes to the file button.
    const rest = this.rows();
    const next = (rest[index] ?? rest[index - 1])?.querySelector<HTMLElement>('[data-gt-remove]');
    (next ?? this.fileInput).focus();
  }

  private dispose(): void {
    this.onForm.abort();
    for (const row of [...this.files.keys()]) this.forget(row);
  }

  private forget(row: HTMLElement): void {
    const kept = this.files.get(row);
    if (kept) URL.revokeObjectURL(kept.url);
    this.files.delete(row);
  }

  private move(row: HTMLElement, by: number, button: Element): void {
    const rows = this.rows();
    const to = rows.indexOf(row) + by;
    const order = moveItem(rows, rows.indexOf(row), to);
    this.list.append(...order);
    this.sync();
    (row.querySelector<HTMLElement>(`[data-gt-move="${by}"]`) ?? (button as HTMLElement)).focus();
  }

  private zoom(row: HTMLElement): void {
    const dialog = this.q<HTMLDialogElement>('[data-gt-lightbox]');
    const img = this.q<HTMLImageElement>('[data-gt-lightbox-image]');
    const thumb = row.querySelector('img');
    img.src = thumb?.src ?? '';
    img.alt = row.querySelector('[data-gt-zoom]')?.getAttribute('title') ?? '';
    dialog.showModal();
  }

  private say(text: string): void {
    const el = this.q('[data-gt-error]');
    el.textContent = text;
    el.hidden = text === '';
  }

  /** Brings everything that follows the rows in line: no duplicates, the limit, the numbers, the names of an upload's fields, the files. */
  private sync(): void {
    const seen = new Set<string>();
    for (const row of this.rows()) {
      const ref = row.dataset.ref ?? '';
      if (!isUploadRef(ref) && seen.has(ref)) {
        row.remove(); // the same image twice is one image
      } else seen.add(ref);
    }
    const rows = this.rows();
    for (const row of rows.slice(this.max)) {
      this.forget(row);
      row.remove();
      this.say(`最大 ${this.max} 枚です`);
    }
    const kept: File[] = [];
    this.rows().forEach((row, i) => {
      row.querySelector<HTMLElement>('[data-gt-badge]')!.textContent = String(i + 1);
      const own = this.files.get(row);
      if (own) {
        kept.push(own.file);
        this.rename(row, uploadRef(kept.length));
      }
    });
    for (const row of [...this.files.keys()]) if (!this.list.contains(row)) this.forget(row);
    this.setFiles(kept);
    releasePicker(this.q('[data-gt-picker-dialog]'), new Set(this.rows().map((r) => r.dataset.ref ?? '')));
    this.q('[data-gt-count]').textContent = `${this.rows().length} / ${this.max}`;
    applyModels(this.root, this.form, this.rows().length);
  }

  /** Names the fields of an image chosen in the browser after its number among the files. */
  private rename(row: HTMLElement, ref: string): void {
    row.dataset.ref = ref;
    row.querySelector<HTMLInputElement>('input[name="ref"]')!.value = ref;
    row.querySelector('[data-gt-role]')?.setAttribute('name', `role_${ref}`);
    row.querySelector('[data-gt-strength]')?.setAttribute('name', `strength_${ref}`);
  }

  private setFiles(files: File[]): void {
    if (typeof DataTransfer === 'undefined') return;
    const dt = new DataTransfer();
    for (const f of files) dt.items.add(f);
    this.fileInput.files = dt.files;
  }
}

function hasFiles(e: DragEvent): boolean {
  return [...(e.dataTransfer?.types ?? [])].includes('Files');
}

/** Starts the input images of every form below `root` that has not been started. */
export function initGentestInputs(root: ParentNode = document): void {
  for (const el of root.querySelectorAll<HTMLElement>('[data-gt-inputs]')) {
    if (inited.has(el)) continue;
    inited.add(el);
    new Inputs(el);
  }
  if (root instanceof HTMLElement && root.matches('[data-gt-inputs]') && !inited.has(root)) {
    inited.add(root);
    new Inputs(root);
  }
}
