/**
 * The "?" help of a screen (spec/components/shared-ui-parts.md §9, molecules/help): a button with `data-help-toggle`
 * whose `aria-controls` names a panel with `data-help-panel` that is `hidden` until the button is pressed. It is a
 * disclosure, not a dialog: the panel opens in the flow of the page under its heading, the focus stays on the button
 * (so that Enter or Space again closes it) and the screen reader is told by `aria-expanded`.
 *
 * - Enter / Space / click toggle the panel and `aria-expanded` together (a `<button>` gives the keys).
 * - Esc closes the panel when the focus is on its button or inside it, and puts the focus on the button.
 * - Helps are independent: opening one does not close another.
 * - It works on controls that arrive later (a swapped body): the listeners are on the document.
 */

const TOGGLE = '[data-help-toggle]';
const PANEL = '[data-help-panel]';

function panelOf(toggle: Element): HTMLElement | null {
  const id = toggle.getAttribute('aria-controls');
  return id ? document.getElementById(id) : null;
}

function set(toggle: HTMLElement, open: boolean): void {
  const panel = panelOf(toggle);
  if (!panel) return;
  panel.hidden = !open;
  toggle.setAttribute('aria-expanded', String(open));
}

/** The button of the open help that the event is on or inside the panel of; null when it is on none. */
function openToggleAt(target: Element | null): HTMLElement | null {
  if (!target) return null;
  const own = target.closest<HTMLElement>(TOGGLE);
  if (own && own.getAttribute('aria-expanded') === 'true') return own;
  const panel = target.closest<HTMLElement>(PANEL);
  if (!panel || panel.hidden || !panel.id) return null;
  return document.querySelector<HTMLElement>(`${TOGGLE}[aria-controls="${CSS.escape(panel.id)}"]`);
}

/** Installs the behaviour of the "?" helps. Call it once (main.ts). */
export function installHelp(root: Document = document): void {
  root.addEventListener('click', (event) => {
    const toggle = (event.target as Element | null)?.closest<HTMLElement>(TOGGLE);
    if (toggle) set(toggle, toggle.getAttribute('aria-expanded') !== 'true');
  });

  root.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || event.defaultPrevented) return;
    const toggle = openToggleAt(event.target as Element | null);
    if (!toggle) return;
    event.preventDefault();
    set(toggle, false);
    toggle.focus();
  });
}
