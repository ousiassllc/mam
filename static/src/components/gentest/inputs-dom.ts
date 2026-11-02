import { dropSide } from './inputs-model';

// The DOM parts of the input images that do not depend on the rest of the state (inputs.ts keeps that): the drop highlight, the
// reordering of the thumbnails by dragging, and the buttons of the picker dialog.

/** Swaps the colour classes of a drop target (the highlights are classes of the colour table, spec/components/color-rules.md). */
export function highlight(el: Element, on: boolean, rest: string, active: string[]): void {
  el.classList.toggle(rest, !on);
  for (const c of active) el.classList.toggle(c, on);
}

const ROW_REST = 'border-neutral-700';
const ROW_ACTIVE = 'border-indigo-400';

/** The reordering of the thumbnails of `list` by dragging one onto another (HTML5 DnD); `changed` runs after a thumbnail moved. */
export class RowReorder {
  private dragging: HTMLElement | null = null;

  constructor(
    list: HTMLElement,
    private readonly rows: () => HTMLElement[],
    private readonly changed: () => void,
  ) {
    list.addEventListener('dragstart', (e) => this.start(e));
    list.addEventListener('dragover', (e) => this.over(e));
    list.addEventListener('drop', (e) => this.drop(e));
    list.addEventListener('dragend', () => this.end());
  }

  /** The thumbnail an event happened in (a drag can start on a text node: not an Element). */
  private rowAt(e: Event): HTMLElement | null {
    return e.target instanceof Element ? e.target.closest<HTMLElement>('[data-gt-ref]') : null;
  }

  private start(e: DragEvent): void {
    const row = this.rowAt(e);
    if (!row) return;
    this.dragging = row;
    row.toggleAttribute('data-dragging', true);
    e.dataTransfer?.setData('text/plain', 'gt-ref');
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
  }

  private over(e: DragEvent): void {
    const over = this.rowAt(e);
    if (!this.dragging || !over || over === this.dragging) return;
    e.preventDefault();
    for (const r of this.rows()) highlight(r, false, ROW_REST, [ROW_ACTIVE]);
    highlight(over, true, ROW_REST, [ROW_ACTIVE]);
  }

  private drop(e: DragEvent): void {
    const over = this.rowAt(e);
    if (!this.dragging || !over || over === this.dragging) return;
    e.preventDefault();
    if (dropSide(over.getBoundingClientRect(), e.clientX) === 'before') over.before(this.dragging);
    else over.after(this.dragging);
    this.end();
    this.changed();
  }

  private end(): void {
    this.dragging?.removeAttribute('data-dragging');
    this.dragging = null;
    for (const r of this.rows()) highlight(r, false, ROW_REST, [ROW_ACTIVE]);
  }
}

/** Marks the "追加" button of the picker whose image was added (the `htmx:afterRequest` of the dialog). */
export function markAdded(e: Event): void {
  const detail = (e as CustomEvent<{ elt?: HTMLElement; successful?: boolean }>).detail;
  const button = detail.elt;
  if (detail.successful && button?.tagName === 'BUTTON' && button.closest('[data-testid="gt-pick"]')) {
    button.textContent = '追加済み';
    button.setAttribute('disabled', '');
  }
}

/** Gives back the "追加" button of a picker image that is no longer among the thumbnails (it was removed). */
export function releasePicker(dialog: Element, chosen: Set<string>): void {
  for (const button of dialog.querySelectorAll<HTMLButtonElement>('[data-testid="gt-pick"] button[data-ref]')) {
    if (button.disabled && !chosen.has(button.dataset.ref ?? '')) {
      button.textContent = '追加';
      button.disabled = false;
    }
  }
}
