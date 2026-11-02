/**
 * The "こだわり" tier of the generation test form (Issue #977): the values that came from the easy tier (or the model's own) are in the
 * controls from the start; a control that differs from where the tier began is marked "変えた" and listed in the panel with a way back for
 * itself, and one for all. The controls are the detailed settings (seed, steps, cfg, sampler, scheduler: advanced.ts), the negative prompts, the
 * LoRA and its strength and the strengths of the input images. Nothing is posted from here: the controls post as they always did.
 * The rules are in refine-model.ts. The listeners are on the document: the form may arrive in a swapped body.
 */

import { changed, changedItems, line, resetLabel, type Item } from './refine-model';

const ROOT = '[data-gt-refine]';

/** A control the tier tracks: how to read it, put a value back and where its mark goes. `el` is the element that remembers where the tier began. */
interface Tracked {
  item: Item;
  el: Element;
  /** Where the "変えた" mark is put: before `host` when `before`, else at the start of it. */
  host: HTMLElement;
  before?: boolean;
  /** A control that is tracked only once the user has moved it (a strength that follows its role until then). */
  follows?: boolean;
  write(value: string): void;
}

const initial = new WeakMap<Element, string>();

const ADV_LABELS: Record<string, string> = {
  seed: 'シード',
  steps: 'ステップ数',
  cfg: 'CFG',
  sampler: 'サンプラー',
  scheduler: 'スケジューラー',
};
const ADV_EMPTY: Record<string, string> = {
  seed: 'ランダム',
  steps: '既定',
  cfg: '既定',
  sampler: '既定',
  scheduler: '既定',
};

function fire(el: Element, type: string): void {
  el.dispatchEvent(new Event(type, { bubbles: true }));
}

function setValue(el: HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement, value: string): void {
  el.value = value;
  if (!(el instanceof HTMLSelectElement)) fire(el, 'input');
  fire(el, 'change');
}

/** The steps and the cfg are sliders that post only while their "上書きする" box is ticked: off reads as "". */
function slider(field: HTMLElement, kind: string): Tracked | null {
  const box = field.querySelector<HTMLInputElement>('[data-gt-adv-on]');
  const range = field.querySelector<HTMLInputElement>('[data-gt-adv-range]');
  if (!box || !range) return null;
  return {
    item: {
      key: `adv:${kind}`,
      label: ADV_LABELS[kind] ?? kind,
      empty: ADV_EMPTY[kind] ?? '既定',
      initial: '',
      current: box.checked ? range.value : '',
    },
    el: box,
    host: field,
    write(value) {
      box.checked = value !== '';
      fire(box, 'change'); // advanced.ts starts a switched-on slider from the model's default...
      if (value !== '') setValue(range, value); // ...and the value of the tier comes after it
    },
  };
}

type Field = HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement;

function control(el: Field, key: string, label: string, empty: string, host: HTMLElement, before = false): Tracked {
  return {
    item: { key, label, empty, initial: '', current: el.value },
    el,
    host,
    before,
    write: (v) => setValue(el, v),
  };
}

/** Every control of the form that the tier tracks. */
function tracked(form: HTMLElement): Tracked[] {
  const out: Tracked[] = [];
  for (const field of form.querySelectorAll<HTMLElement>('[data-gt-adv-field]')) {
    const kind = field.dataset.gtAdvField ?? '';
    if (kind === 'steps' || kind === 'cfg') {
      const t = slider(field, kind);
      if (t) out.push(t);
      continue;
    }
    const el = field.querySelector<HTMLInputElement | HTMLSelectElement>('input[name],select[name]');
    if (el) out.push(control(el, `adv:${kind}`, ADV_LABELS[kind] ?? kind, ADV_EMPTY[kind] ?? '既定', field));
  }
  form.querySelectorAll<HTMLTextAreaElement>('textarea[name="negative"]').forEach((el, i) => {
    out.push(control(el, `negative:${i}`, 'ネガティブプロンプト', 'なし', el, true));
  });
  const lora = form.querySelector<HTMLSelectElement>('select[name="lora"]');
  if (lora) out.push(control(lora, 'lora', 'LoRA', 'なし', lora.closest<HTMLElement>('[data-gt-lora]') ?? form));
  const strength = form.querySelector<HTMLInputElement>('input[name="lora_strength"]');
  if (strength) out.push(control(strength, 'lora:strength', 'LoRA の強さ', '既定', strength.parentElement ?? form));
  form.querySelectorAll<HTMLInputElement>('input[type="range"][name^="strength_"]').forEach((el, i) => {
    const t = control(
      el,
      `ref:${i}`,
      `参照画像の強さ（${i + 1} 枚目）`,
      '既定',
      el.closest<HTMLElement>('[data-gt-ref]') ?? form,
    );
    t.follows = true;
    out.push(t);
  });
  return out;
}

/**
 * Fills in what the tier began with: the first value a control is seen with. A strength of an input image that the user has not moved
 * follows its role (a face starts higher), so it is where it began for as long as it is not moved.
 */
function withInitial(t: Tracked): Tracked {
  const untouched = t.follows === true && (t.el as HTMLElement).dataset.touched === undefined;
  if (!initial.has(t.el) || untouched) initial.set(t.el, t.item.current);
  t.item.initial = initial.get(t.el) ?? '';
  return t;
}

function all(form: HTMLElement): Tracked[] {
  return tracked(form).map(withInitial);
}

function mark(t: Tracked, on: boolean): void {
  t.host.toggleAttribute('data-gt-changed', on);
  const where = t.before ? t.host.previousElementSibling : t.host.querySelector(':scope > [data-gt-mark]');
  const existing = where?.matches('[data-gt-mark]') ? where : null;
  if (on && !existing) {
    const badge = document.createElement('span');
    badge.setAttribute('data-gt-mark', '');
    badge.className = 'text-xs text-amber-300';
    badge.textContent = '● 変えた';
    if (t.before) t.host.before(badge);
    else t.host.prepend(badge);
  } else if (!on && existing) {
    existing.remove();
  }
}

function render(section: HTMLElement): void {
  const form = section.closest<HTMLElement>('form');
  if (!form) return;
  const items = all(form);
  for (const t of items) mark(t, changed(t.item));
  const list = section.querySelector<HTMLElement>('[data-gt-refine-list]');
  const empty = section.querySelector<HTMLElement>('[data-gt-refine-empty]');
  const resetAll = section.querySelector<HTMLElement>('[data-gt-reset-all]');
  const diff = changedItems(items.map((t) => t.item));
  if (empty) empty.hidden = diff.length > 0;
  if (resetAll) resetAll.hidden = diff.length === 0;
  if (!list) return;
  const rows = [...list.children] as HTMLElement[];
  if (rows.length === diff.length && rows.every((li, i) => li.dataset.gtRefineItem === diff[i]?.key)) {
    // The same items as before: only the words that moved are written, so the live region and the focus on a button stay as they are.
    diff.forEach((item, i) => {
      const span = rows[i]?.querySelector('span');
      if (span && span.textContent !== line(item)) span.textContent = line(item);
    });
    return;
  }
  list.replaceChildren(...diff.map(row));
}

function row(item: Item): HTMLElement {
  const li = document.createElement('li');
  li.className = 'flex flex-wrap items-center justify-between gap-2 text-xs text-neutral-200';
  li.dataset.gtRefineItem = item.key;
  const text = document.createElement('span');
  text.className = 'break-words';
  text.textContent = line(item);
  const button = document.createElement('button');
  button.type = 'button';
  button.dataset.gtReset = item.key;
  button.className = 'rounded-md border border-neutral-600 px-2 py-0.5 text-neutral-200 hover:bg-neutral-800';
  button.textContent = '元に戻す';
  button.setAttribute('aria-label', resetLabel(item));
  li.append(text, button);
  return li;
}

/**
 * After a reset the button that was pressed is gone (or hidden) and the focus fell to the page: it goes to the "元に戻す" of the item
 * that followed, else to "全部を元に戻す", else to the heading of the section (no item is left to reach).
 */
function refocus(section: HTMLElement, pressed: HTMLElement, nextKey: string | undefined): void {
  if (pressed.isConnected && !pressed.closest('[hidden]')) return;
  const next =
    nextKey === undefined
      ? null
      : [...section.querySelectorAll<HTMLElement>('[data-gt-reset]')].find((b) => b.dataset.gtReset === nextKey);
  const resetAll = section.querySelector<HTMLElement>('[data-gt-reset-all]');
  const heading = document.getElementById(section.getAttribute('aria-labelledby') ?? '');
  heading?.setAttribute('tabindex', '-1');
  (next ?? (resetAll && !resetAll.hidden ? resetAll : heading))?.focus();
}

function reset(section: HTMLElement, key: string | null): void {
  const form = section.closest<HTMLElement>('form');
  if (!form) return;
  for (const t of all(form)) {
    if ((key === null || t.item.key === key) && changed(t.item)) t.write(t.item.initial);
  }
  render(section);
}

function sections(root: ParentNode): HTMLElement[] {
  const own = root instanceof HTMLElement && root.matches(ROOT) ? [root] : [];
  return [...own, ...root.querySelectorAll<HTMLElement>(ROOT)];
}

function renderAll(root: ParentNode = document): void {
  for (const section of sections(root)) render(section);
}

/** Installs the marks and the way back. Call it once (main.ts). */
export function initGentestRefine(root: Document = document): void {
  const refresh = (event: Event) => {
    const form = (event.target as Element | null)?.closest('form');
    const section = form?.querySelector<HTMLElement>(ROOT);
    if (section) render(section);
  };
  root.addEventListener('input', refresh);
  root.addEventListener('change', refresh);
  root.addEventListener('click', (event) => {
    const el = (event.target as Element | null)?.closest<HTMLElement>('[data-gt-reset],[data-gt-reset-all]');
    const section = el?.closest<HTMLElement>(ROOT);
    if (!el || !section) return;
    const nextKey = el
      .closest<HTMLElement>('[data-gt-refine-item]')
      ?.nextElementSibling?.getAttribute('data-gt-refine-item');
    reset(section, el.matches('[data-gt-reset-all]') ? null : (el.dataset.gtReset ?? null));
    refocus(section, el, nextKey ?? undefined);
  });
  // a row of an input image, or the models that decide which settings are shown, arrive after the first look
  const later = () => window.setTimeout(() => renderAll(root), 0);
  root.addEventListener('htmx:load', later);
  root.addEventListener('DOMContentLoaded', later);
}
