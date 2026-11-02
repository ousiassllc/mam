/**
 * The filter of commercial use (#928), one component for every screen that lists models.
 *
 * A group of radio buttons marked `data-commercial-filter` (the molecule `commercial.Filter`) names the list it narrows with
 * `data-commercial-filter-target="<css selector>"`. Inside that list, every element with `data-commercial="yes|conditional|unknown|no"`
 * (a model card, a table row, an `<option>`) is an item: an item whose answer the chosen filter does not allow is hidden.
 *
 * - A hidden item is also disabled (its controls post nothing), and shown and enabled again when the filter is widened. A control that was
 *   disabled by the server stays disabled: only what this component disabled (`data-commercial-off`) is given back.
 * - An `<option>` that is the selected one is never hidden: a saved choice is not changed behind the user's back.
 * - The line `[data-commercial-filter-status]` inside the group says how many items are hidden.
 * - After a change the target gets a bubbling `commercial-filter-applied` event, for the code that reads the list (the generation test
 *   counts the ticked models).
 *
 * The component only reads the answers the server drew; the server also draws the filter with the default (the setting
 * "商用OKのモデルだけを使う") checked.
 */

/** The answers each filter lets through (modellicense.Filter). */
const ALL_ANSWERS: readonly string[] = ['yes', 'conditional', 'unknown', 'no'];
const ALLOWED: Readonly<Record<string, readonly string[]>> = {
  all: ALL_ANSWERS,
  ok: ['yes'],
  ok_or_conditional: ['yes', 'conditional'],
};

const FORM_CONTROLS = 'input,select,textarea,button';

function chosenFilter(group: HTMLElement): string {
  return group.querySelector<HTMLInputElement>('input[type="radio"]:checked')?.value ?? 'all';
}

/** Hides or shows one item; a hidden item's controls are disabled by this component and given back when it is shown. */
function setItemShown(item: HTMLElement, shown: boolean): void {
  item.hidden = !shown;
  if (item instanceof HTMLOptionElement) {
    if (!shown) {
      item.disabled = true;
      item.dataset.commercialOff = '';
    } else if ('commercialOff' in item.dataset) {
      item.disabled = false;
      delete item.dataset.commercialOff;
    }
    return;
  }
  for (const control of item.querySelectorAll<HTMLElement & { disabled: boolean }>(FORM_CONTROLS)) {
    if (!shown && !control.disabled) {
      control.disabled = true;
      control.dataset.commercialOff = '';
    } else if (shown && 'commercialOff' in control.dataset) {
      control.disabled = false;
      delete control.dataset.commercialOff;
    }
  }
}

/** Applies the filter chosen in `group` to its target. */
export function applyCommercialFilter(group: HTMLElement): void {
  const selector = group.dataset.commercialFilterTarget;
  const target = selector ? document.querySelector<HTMLElement>(selector) : null;
  if (!target) return;
  const allowed = ALLOWED[chosenFilter(group)] ?? ALL_ANSWERS;
  let hidden = 0;
  for (const item of target.querySelectorAll<HTMLElement>('[data-commercial]')) {
    const keepSelected = item instanceof HTMLOptionElement && item.selected;
    const shown = keepSelected || allowed.includes(item.dataset.commercial ?? 'unknown');
    if (!shown) hidden++;
    setItemShown(item, shown);
  }
  const status = group.querySelector<HTMLElement>('[data-commercial-filter-status]');
  if (status) status.textContent = hidden > 0 ? `${hidden} 件を隠しています` : '';
  target.dispatchEvent(new CustomEvent('commercial-filter-applied', { bubbles: true }));
}

/** Applies every filter of `root` (the page, or a swapped fragment). */
export function applyCommercialFilters(root: ParentNode = document): void {
  for (const group of root.querySelectorAll<HTMLElement>('[data-commercial-filter]')) applyCommercialFilter(group);
}

const installed = new WeakSet<Document>();

/**
 * Installs the filters. Call it once (main.ts): a choice applies at once, and the filters are applied again when the page has loaded
 * and whenever htmx loads something (a list that was drawn again arrives unfiltered).
 */
export function installCommercialFilter(root: Document = document): void {
  if (installed.has(root)) return;
  installed.add(root);
  root.addEventListener('change', (event) => {
    const group = (event.target as Element | null)?.closest<HTMLElement>('[data-commercial-filter]');
    if (group) applyCommercialFilter(group);
  });
  root.addEventListener('DOMContentLoaded', () => applyCommercialFilters(root));
  root.addEventListener('htmx:load', () => applyCommercialFilters(root));
}
