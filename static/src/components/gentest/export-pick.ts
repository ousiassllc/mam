/**
 * The choice of results to save as a ZIP on the history of the generation test (#932).
 *
 * A card of the history that made a file (`[data-gt-pick="<result id>"]`, the Templ `HistoryCard`) is chosen by a tap, as the model cards
 * of the form are (#906): the card holds an `sr-only` checkbox (`[data-gt-pick-input]`) that is the peer of its frame, so a chosen card
 * glows (`peer-checked:`), and the keyboard (Tab, Space) and a screen reader get a real checkbox. A tap on something that does
 * something else (the detail link, the heart, the player) is left to it.
 *
 * The choice is kept here, in memory, per history (`#gt-history`), not in the cards: the part of the history that polls while
 * a result runs is swapped every 2 seconds and its cards are drawn again, so after every swap the choice is put on the new cards
 * (and a result that is gone from the list is dropped). The export bar (`[data-gt-export-selected]`) lies outside of that part: its
 * button shows how many are chosen, is off while none is, and its hidden fields `id` carry the chosen results of the cards that are
 * shown (the commercial-use filter hides cards; what is hidden is not saved).
 * The second form of the bar (the newest N of the filter) learns the commercial-use choice of the filter, which is only applied in
 * the page: its hidden field `commercial` follows the radio.
 *
 * The bar asks the server through htmx before it downloads: a choice that cannot be saved is said in the line of the bar
 * (`[data-gt-export-status]`) and the page stays. The reason of the last refusal is cleared when the next ask starts.
 */

const ROOT = '#gt-history';
const INTERACTIVE = 'a, button, video, audio, input, select, textarea, summary, label';

const choices = new WeakMap<Element, Set<string>>();

function choiceOf(root: Element): Set<string> {
  let set = choices.get(root);
  if (!set) {
    set = new Set();
    choices.set(root, set);
  }
  return set;
}

/** The chosen results whose card is in the list and shown. */
function chosenShown(root: Element): string[] {
  const set = choiceOf(root);
  return [...root.querySelectorAll<HTMLElement>('[data-gt-pick]')]
    .filter((card) => !card.closest('[hidden]'))
    .map((card) => card.dataset.gtPick ?? '')
    .filter((id) => set.has(id));
}

/** Puts the choice on the cards and on the export bar. */
export function renderChoice(root: Element): void {
  const set = choiceOf(root);
  const present = new Set<string>();
  for (const card of root.querySelectorAll<HTMLElement>('[data-gt-pick]')) {
    const id = card.dataset.gtPick ?? '';
    present.add(id);
    const input = card.querySelector<HTMLInputElement>('[data-gt-pick-input]');
    if (input) input.checked = set.has(id);
  }
  for (const id of [...set]) if (!present.has(id)) set.delete(id);

  const ids = chosenShown(root);
  const form = root.querySelector<HTMLFormElement>('[data-gt-export-selected]');
  if (form) {
    for (const old of form.querySelectorAll('input[data-gt-id]')) old.remove();
    for (const id of ids) {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = 'id';
      input.value = id;
      input.dataset.gtId = '';
      form.append(input);
    }
    const count = form.querySelector<HTMLElement>('[data-gt-selected-count]');
    if (count) count.textContent = String(ids.length);
    const submit = form.querySelector<HTMLButtonElement>('[data-gt-export-submit]');
    if (submit) submit.disabled = ids.length === 0;
    const clear = form.querySelector<HTMLElement>('[data-gt-select-clear]');
    if (clear) clear.hidden = ids.length === 0;
  }
  const commercial = root.querySelector<HTMLInputElement>(
    '[data-commercial-filter] input[type="radio"]:checked',
  )?.value;
  if (commercial) {
    for (const input of root.querySelectorAll<HTMLInputElement>('input[data-gt-export-commercial]')) {
      input.value = commercial;
    }
  }
}

function renderAll(): void {
  for (const root of document.querySelectorAll(ROOT)) renderChoice(root);
}

const installed = new WeakSet<Document>();

/**
 * Installs the choice. Call it once (main.ts): the taps work for the history of any page and swap, and the choice is put on again after
 * every swap and after the commercial-use filter changed what is shown.
 */
export function installExportPick(doc: Document = document): void {
  if (installed.has(doc)) return;
  installed.add(doc);
  doc.addEventListener('click', (event) => {
    const target = event.target as Element | null;
    const root = target?.closest(ROOT);
    if (!root || !target) return;
    if (target.closest('[data-gt-select-clear]')) {
      choiceOf(root).clear();
      renderChoice(root);
      return;
    }
    const card = target.closest<HTMLElement>('[data-gt-pick]');
    const input = card?.querySelector<HTMLInputElement>('[data-gt-pick-input]');
    if (!card || !input || target.closest(INTERACTIVE)) return;
    input.checked = !input.checked;
    input.dispatchEvent(new Event('change', { bubbles: true }));
  });
  // The checkbox is the source of the change: a tap above, or Space on the focused checkbox.
  doc.addEventListener('change', (event) => {
    const input = (event.target as Element | null)?.closest<HTMLInputElement>('[data-gt-pick-input]');
    const root = input?.closest(ROOT);
    const id = input?.closest<HTMLElement>('[data-gt-pick]')?.dataset.gtPick;
    if (!input || !root || !id) return;
    const set = choiceOf(root);
    if (input.checked) set.add(id);
    else set.delete(id);
    renderChoice(root);
  });
  doc.addEventListener('htmx:beforeRequest', (event) => {
    const status = (event.target as Element | null)
      ?.closest('[data-gt-export-bar]')
      ?.querySelector('[data-gt-export-status]');
    status?.replaceChildren();
  });
  doc.addEventListener('htmx:afterSwap', renderAll);
  doc.addEventListener('htmx:load', renderAll);
  doc.addEventListener('commercial-filter-applied', renderAll);
  doc.addEventListener('DOMContentLoaded', renderAll);
}
