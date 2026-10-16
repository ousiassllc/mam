/**
 * Keeps the keyboard focus across an HTMX swap that replaces the control the user just used.
 *
 * Most actions answer with the region they sit in (the LockToggle form, a list with its Delete buttons,
 * a panel), swapped by `outerHTML`. htmx puts the focus back only on an element with the same `id`; the
 * buttons have none, so the focus fell to <body> and a keyboard or screen reader user had to Tab from the
 * top of the page after every press (WCAG 2.4.3).
 *
 * Only a swap that the user started (a click, a submit, a change, a key press) and that destroyed the
 * focused element inside the swapped region is acted on. Polling, `load`/`revealed` triggers and
 * WebSocket-driven refreshes carry no such event, and a swap that left the focus alone, or after which
 * the user (or htmx, by `id`) has put the focus somewhere, is never touched.
 */

const FOCUSABLE =
  'a[href],button,input:not([type="hidden"]),select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])';

/** The events of a person using the page. A request fired by a poll or `load` has none of them. */
const USER_EVENTS: ReadonlySet<string> = new Set([
  'click',
  'dblclick',
  'submit',
  'change',
  'input',
  'keydown',
  'keyup',
]);

const HX_ACTIONS = ['hx-get', 'hx-post', 'hx-put', 'hx-patch', 'hx-delete'] as const;

interface BeforeSwapDetail {
  xhr: XMLHttpRequest;
  target: Element;
  requestConfig?: { triggeringEvent?: Event | null };
}

/** What was focused before the swap, and where it sat. */
interface FocusMemory {
  xhr: XMLHttpRequest;
  active: Element;
  target: Element;
  parent: ParentNode | null;
  before: ChildNode | null;
  after: ChildNode | null;
  key: string;
  keyIndex: number;
  kind: string;
  kindIndex: number;
}

/** The focusable elements of `roots` and their descendants, in document order. */
function focusablesOf(roots: readonly Element[]): Element[] {
  return roots.flatMap((root) => [...(root.matches(FOCUSABLE) ? [root] : []), ...root.querySelectorAll(FOCUSABLE)]);
}

/** What makes a control this control: what it is, what it sends and where, and what it says. */
function keyOf(el: Element): string {
  const attr = (name: string) => el.getAttribute(name) ?? '';
  return [
    el.tagName,
    attr('type'),
    attr('name'),
    attr('value'),
    attr('aria-label'),
    attr('data-focus-key'),
    ...HX_ACTIONS.map(attr),
    el.textContent?.trim() ?? '',
  ].join('\u0000');
}

/** The same without what differs between the rows of a list (its URL, its label): the "kind" of control. */
function kindOf(el: Element): string {
  return [el.tagName, el.getAttribute('type') ?? '', el.getAttribute('name') ?? '', el.textContent?.trim() ?? ''].join(
    '\u0000',
  );
}

function userStarted(detail: BeforeSwapDetail): boolean {
  const event = detail.requestConfig?.triggeringEvent;
  return !!event && USER_EVENTS.has(event.type);
}

function remember(detail: BeforeSwapDetail): FocusMemory | null {
  const active = document.activeElement;
  const { target } = detail;
  if (!active || active === document.body || !target.contains(active) || !userStarted(detail)) return null;
  const candidates = focusablesOf([target]);
  const key = keyOf(active);
  const kind = kindOf(active);
  return {
    xhr: detail.xhr,
    active,
    target,
    parent: target.parentNode,
    before: target.previousSibling,
    after: target.nextSibling,
    key,
    keyIndex: candidates.filter((el) => el !== active && keyOf(el) === key && precedes(el, active)).length,
    kind,
    kindIndex: candidates.filter((el) => el !== active && kindOf(el) === kind && precedes(el, active)).length,
  };
}

function precedes(a: Node, b: Node): boolean {
  return !!(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
}

/** The elements now where the swapped target was: the target itself when only its content changed. */
function swappedRoots(memory: FocusMemory): Element[] {
  const { target, parent, before, after } = memory;
  if (target.isConnected) return [target];
  if (!(parent instanceof Element) || !parent.isConnected) return [];
  if (before && before.parentNode !== parent) return [parent];
  if (after && after.parentNode !== parent) return [parent];
  const roots: Element[] = [];
  for (let node = before ? before.nextSibling : parent.firstChild; node && node !== after; node = node.nextSibling) {
    if (node instanceof Element) roots.push(node);
  }
  return roots;
}

function focus(el: Element): void {
  if (!(el instanceof HTMLElement)) return;
  el.focus({ preventScroll: true });
}

/** The nth of `matches`, or the last one when the list got shorter (the control after a deleted row). */
function nth(matches: Element[], index: number): Element | undefined {
  return matches[Math.min(index, matches.length - 1)];
}

function restore(memory: FocusMemory): void {
  // The element survived (a swap that only added), or htmx or the user already put the focus somewhere.
  if (memory.active.isConnected) return;
  const current = document.activeElement;
  if (current && current !== document.body) return;

  const roots = swappedRoots(memory);
  const candidates = focusablesOf(roots).filter((el) => !el.matches(':disabled'));
  const same = candidates.filter((el) => keyOf(el) === memory.key);
  const sameKind = candidates.filter((el) => kindOf(el) === memory.kind);
  const next = nth(same, memory.keyIndex) ?? nth(sameKind, memory.kindIndex);
  if (next) {
    focus(next);
    return;
  }

  // Nothing to press again (the last row was deleted): the region itself, so that a reader starts there.
  const region = roots[0] ?? (memory.parent instanceof Element ? memory.parent : null);
  if (!region || region === document.body) return;
  if (!region.hasAttribute('tabindex')) region.setAttribute('tabindex', '-1');
  focus(region);
}

/** Gives the focus back after the swaps that the user started and that replaced the focused control. */
export function installFocusRestore(root: Document | HTMLElement = document): void {
  let pending: FocusMemory | null = null;

  root.addEventListener('htmx:beforeSwap', (event) => {
    pending = remember((event as CustomEvent<BeforeSwapDetail>).detail);
  });
  root.addEventListener('htmx:afterSwap', (event) => {
    const detail = (event as CustomEvent<{ xhr?: XMLHttpRequest }>).detail;
    if (!pending || detail?.xhr !== pending.xhr) return;
    const memory = pending;
    pending = null;
    restore(memory);
  });
}
