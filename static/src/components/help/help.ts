/**
 * The "?" help of a screen (spec/components/shared-ui-parts.md §9, molecules/help): a button with `data-help-toggle` and
 * `popovertarget` that opens a native popover (`popover`, `data-help-panel`) by it. The browser does what a popover is
 * for: it is in the top layer (no parent with `overflow` clips it), one help is open at a time (opening another closes
 * it), a click or tap outside closes it, and the button toggles it from a click, Enter or Space. This file adds what the
 * browser does not:
 *
 * - `aria-expanded` follows the popover (it is closed in the markup).
 * - The popover is put by its button and kept inside the window: under the button, left edges aligned, shifted left to fit
 *   the width; flipped above the button when there is no room below; as a last resort it sits at the bottom of the window
 *   (it scrolls inside, `max-height` in the markup). It is put again when the window is resized or the page scrolls.
 * - Esc closes it when the focus is on its button, in it or nowhere (on the page), and puts the focus on the button (the
 *   browser's own return is not reliable: a button pressed by touch or by Safari's click is not focused).
 * - The focus is not moved into the popover (it is not a dialog), so Tab from the button goes on to what is next in the
 *   page, which is the content of the popover (its links) when it is open. When the focus goes to anything else, the
 *   popover closes (the keyboard counterpart of the click outside).
 *
 * - A panel that is outside what a swap replaces stays open across it (a help of a polled card): `htmx:afterSettle`
 *   gives the new button its `aria-expanded` and puts the panel by it again. A panel inside a swapped body is new, so closed.
 *
 * Listeners are on the document: a help in a swapped body works too.
 */

const TOGGLE = '[data-help-toggle]';
const PANEL = '[data-help-panel]';
const GAP = 6;
const EDGE = 8;

function toggleOf(panel: Element): HTMLElement | null {
  return panel.id ? document.querySelector<HTMLElement>(`${TOGGLE}[aria-controls="${CSS.escape(panel.id)}"]`) : null;
}

function openPanel(): HTMLElement | null {
  return document.querySelector<HTMLElement>(`${PANEL}:popover-open`);
}

/** Puts the open `panel` by `toggle`, inside the window. */
function place(panel: HTMLElement, toggle: HTMLElement): void {
  const width = document.documentElement.clientWidth;
  const height = window.innerHeight;
  const at = toggle.getBoundingClientRect();
  const w = panel.offsetWidth;
  const h = panel.offsetHeight;
  const left = Math.max(EDGE, Math.min(at.left, width - w - EDGE));
  let top = at.bottom + GAP;
  if (top + h > height - EDGE) {
    const above = at.top - GAP - h;
    top = above >= EDGE ? above : Math.max(EDGE, height - h - EDGE);
  }
  panel.style.left = `${left}px`;
  panel.style.top = `${top}px`;
}

function replace(): void {
  const panel = openPanel();
  const toggle = panel ? toggleOf(panel) : null;
  if (panel && toggle) place(panel, toggle);
}

/**
 * After a swap, tells a button its panel is open. A panel kept out of the swapped region (a help of a polled card) stays
 * open while its button is drawn again, closed in the markup: the button of an open panel says so, and the panel is put
 * by the new button.
 */
function syncToggles(): void {
  for (const toggle of document.querySelectorAll<HTMLElement>(TOGGLE)) {
    const id = toggle.getAttribute('aria-controls');
    const panel = id ? document.getElementById(id) : null;
    const open = !!panel?.matches(':popover-open');
    const was = toggle.getAttribute('aria-expanded') === 'true';
    if (open !== was) toggle.setAttribute('aria-expanded', String(open));
  }
  replace();
}

/** Installs the behaviour of the "?" helps. Call it once (main.ts). */
export function installHelp(root: Document = document): void {
  // `toggle` does not bubble: listen while it goes down.
  root.addEventListener(
    'toggle',
    (event) => {
      const panel = event.target as Element | null;
      if (!panel?.matches(PANEL)) return;
      const toggle = toggleOf(panel);
      const open = (event as ToggleEvent).newState === 'open';
      toggle?.setAttribute('aria-expanded', String(open));
      if (open && toggle) place(panel as HTMLElement, toggle);
    },
    true,
  );

  root.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || event.defaultPrevented) return;
    const panel = openPanel();
    if (!panel) return;
    const target = event.target as Element | null;
    if (target && target !== document.body && !panel.contains(target) && !target.closest(TOGGLE)) return;
    event.preventDefault();
    panel.hidePopover();
    toggleOf(panel)?.focus();
  });

  root.addEventListener('focusin', (event) => {
    const panel = openPanel();
    const target = event.target as Element | null;
    if (!panel || !target || panel.contains(target) || toggleOf(panel)?.contains(target)) return;
    panel.hidePopover();
  });

  // htmx puts the new content in and settles it (the buttons of a swapped card are new elements).
  root.addEventListener('htmx:afterSettle', syncToggles);
  window.addEventListener('resize', replace);
  // The popover is fixed in the window and the button moves with the page: follow it (a scroll inside the popover is not
  // a move of the button, and putting it again there changes nothing).
  root.addEventListener('scroll', replace, true);
}
