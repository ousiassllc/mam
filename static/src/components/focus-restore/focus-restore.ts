/**
 * Keeps the keyboard focus across an HTMX request that replaces (or disables) the control the user just used.
 *
 * Most actions answer with the region they sit in (the LockToggle form, a list with its Delete buttons,
 * a panel), swapped by `outerHTML` or by `hx-swap-oob`. htmx puts the focus back only on an element with the
 * same `id`; the buttons have none, so the focus fell to <body> and a keyboard or screen reader user had to
 * Tab from the top of the page after every press (WCAG 2.4.3).
 *
 * Only a request that the user started (a click, a submit, a change, a key press, or a request such a request
 * set off through `HX-Trigger`) and that destroyed the focused control is acted on. Polling, `load`/`revealed`
 * triggers and WebSocket-driven refreshes carry no such event, and a request after which the user (or htmx,
 * by `id`) has put the focus somewhere is never touched.
 *
 * The focused control is read when the request leaves (`htmx:beforeRequest`), not when the response comes
 * back: `hx-disabled-elt` disables the pressed button right after that, and Chrome drops the focus of a
 * button that becomes disabled to <body>.
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

/** How long a request of the user keeps counting as in flight: `htmx:afterRequest` ends it, but a hung one must expire. */
const MAX_REQUEST_MS = 30_000;

interface RequestDetail {
  xhr: XMLHttpRequest;
  target: Element;
  requestConfig?: { triggeringEvent?: Event | null };
}

/** The focusable elements of the scope where `active` sat, and how many controls like it came before it. */
interface Scope {
  root: Element;
  keyIndex: number;
  kindIndex: number;
}

/** What was focused before the swap, and where it sat. */
interface FocusMemory {
  xhr: XMLHttpRequest;
  active: Element;
  key: string;
  kind: string;
  /** The swap target when it held the focus: replaced in place, so its position tells what took its place. */
  target: Scope | null;
  parent: ParentNode | null;
  before: ChildNode | null;
  after: ChildNode | null;
  /** The ancestors with an `id`, nearest first: an `hx-swap-oob` element is replaced by the one with its id. */
  ancestors: (Scope & { id: string })[];
}

/** The focusable elements of `roots` and their descendants, in document order. */
function focusablesOf(roots: readonly Element[]): Element[] {
  return roots.flatMap((root) => [...(root.matches(FOCUSABLE) ? [root] : []), ...root.querySelectorAll(FOCUSABLE)]);
}

/**
 * What makes a control this control: what it is, what it sends and where, and what it says. A control with
 * `data-focus-key` is told apart by that alone, so a toggle whose label (and `aria-label`) flips on every press
 * ("解決済みにする" / "未解決に戻す") is still found again after the swap.
 */
function keyOf(el: Element): string {
  const focusKey = el.getAttribute('data-focus-key');
  if (focusKey !== null) return [el.tagName, focusKey].join('\u0000');
  const attr = (name: string) => el.getAttribute(name) ?? '';
  return [
    el.tagName,
    attr('type'),
    attr('name'),
    attr('value'),
    attr('aria-label'),
    ...HX_ACTIONS.map(attr),
    el.textContent?.trim() ?? '',
  ].join('\u0000');
}

/** The same without what differs between the rows of a list (its URL, its label): the "kind" of control. */
function kindOf(el: Element): string {
  const focusKey = el.getAttribute('data-focus-key');
  if (focusKey !== null) return [el.tagName, focusKey].join('\u0000');
  return [el.tagName, el.getAttribute('type') ?? '', el.getAttribute('name') ?? '', el.textContent?.trim() ?? ''].join(
    '\u0000',
  );
}

function userStarted(detail: RequestDetail): boolean {
  const event = detail.requestConfig?.triggeringEvent;
  return !!event && USER_EVENTS.has(event.type);
}

function precedes(a: Node, b: Node): boolean {
  return !!(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
}

function scopeOf(root: Element, active: Element, key: string, kind: string): Scope {
  const candidates = focusablesOf([root]).filter((el) => el !== active && precedes(el, active));
  return {
    root,
    keyIndex: candidates.filter((el) => keyOf(el) === key).length,
    kindIndex: candidates.filter((el) => kindOf(el) === kind).length,
  };
}

function ancestorsWithId(active: Element, key: string, kind: string): FocusMemory['ancestors'] {
  const found: FocusMemory['ancestors'] = [];
  for (let el = active.parentElement; el && el !== document.body; el = el.parentElement) {
    if (el.id) found.push({ id: el.id, ...scopeOf(el, active, key, kind) });
  }
  return found;
}

/**
 * The control that had the focus when the response came: the one the user pressed (still focused, or
 * disabled by `hx-disabled-elt` and so dropped to <body>), or the one that took its place in an earlier
 * swap of the same action. Focus the user moved elsewhere in the meantime is not ours.
 */
function focusedFor(pressed: Element, target: Element): Element | null {
  const current = document.activeElement;
  if (!current || current === document.body) return pressed.isConnected ? pressed : null;
  return current === pressed || target.contains(current) ? current : null;
}

function remember(detail: RequestDetail, pressed: Element): FocusMemory | null {
  const active = focusedFor(pressed, detail.target);
  if (!active) return null;
  const key = keyOf(active);
  const kind = kindOf(active);
  const { target } = detail;
  return {
    xhr: detail.xhr,
    active,
    key,
    kind,
    target: target.contains(active) ? scopeOf(target, active, key, kind) : null,
    parent: target.parentNode,
    before: target.previousSibling,
    after: target.nextSibling,
    ancestors: ancestorsWithId(active, key, kind),
  };
}

/** The elements now where the swapped target was: the target itself when only its content changed. */
function swappedRoots(memory: FocusMemory, target: Element): Element[] {
  const { parent, before, after } = memory;
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

/** Where the focused control can have gone: the swap target, then the `hx-swap-oob` elements that replaced its ancestors. */
function replacements(memory: FocusMemory): { roots: Element[]; keyIndex: number; kindIndex: number }[] {
  const scopes = [];
  if (memory.target) scopes.push({ ...memory.target, roots: swappedRoots(memory, memory.target.root) });
  for (const ancestor of memory.ancestors) {
    const now = document.getElementById(ancestor.id);
    if (now && now !== ancestor.root && now.isConnected) scopes.push({ ...ancestor, roots: [now] });
  }
  return scopes;
}

function focus(el: Element): void {
  if (!(el instanceof HTMLElement)) return;
  el.focus({ preventScroll: true });
}

/** The nth of `matches`, or the last one when the list got shorter (the control after a deleted row). */
function nth(matches: Element[], index: number): Element | undefined {
  return matches[Math.min(index, matches.length - 1)];
}

function lostFocus(): boolean {
  const current = document.activeElement;
  return !current || current === document.body;
}

function restore(memory: FocusMemory): void {
  // The element survived (a swap that only added; `afterRequest` gives it the focus back), or htmx or the
  // user already put the focus somewhere.
  if (memory.active.isConnected || !lostFocus()) return;

  const scopes = replacements(memory);
  for (const { roots, keyIndex, kindIndex } of scopes) {
    const candidates = focusablesOf(roots).filter((el) => !el.matches(':disabled'));
    const same = candidates.filter((el) => keyOf(el) === memory.key);
    const sameKind = candidates.filter((el) => kindOf(el) === memory.kind);
    const next = nth(same, keyIndex) ?? nth(sameKind, kindIndex);
    if (next) {
      focus(next);
      return;
    }
  }

  // Nothing to press again (the last row was deleted): the region itself, so that a reader starts there.
  const region =
    scopes.find((scope) => scope.roots[0])?.roots[0] ?? (memory.parent instanceof Element ? memory.parent : null);
  if (!region || region === document.body) return;
  if (!region.hasAttribute('tabindex')) region.setAttribute('tabindex', '-1');
  focus(region);
}

/** Gives the focus back after the requests that the user started and that replaced or disabled the focused control. */
export function installFocusRestore(root: Document | HTMLElement = document): void {
  /** The control focused when each request of the user left. */
  const pressed = new WeakMap<XMLHttpRequest, Element>();
  /** The requests of the user that have not ended (their answer may set off another one: `HX-Trigger`), and until when. */
  const inFlight = new Map<XMLHttpRequest, number>();
  let pending: FocusMemory | null = null;

  const detailOf = (event: Event) => (event as CustomEvent<RequestDetail>).detail;

  function latestInFlight(): XMLHttpRequest | undefined {
    const now = performance.now();
    for (const [xhr, until] of inFlight) if (until <= now) inFlight.delete(xhr);
    return [...inFlight.keys()].pop();
  }

  root.addEventListener('htmx:beforeRequest', (event) => {
    const detail = detailOf(event);
    const parent = latestInFlight();
    // A request that the answer of a user's request set off (`HX-Trigger`) carries a custom event, not the click.
    if (!userStarted(detail) && !parent) return;
    const active = document.activeElement;
    const control = active && active !== document.body ? active : parent && pressed.get(parent);
    if (!control) return;
    pressed.set(detail.xhr, control);
    inFlight.set(detail.xhr, performance.now() + MAX_REQUEST_MS);
  });
  root.addEventListener('htmx:beforeSwap', (event) => {
    const detail = detailOf(event);
    const control = pressed.get(detail.xhr);
    pending = control ? remember(detail, control) : null;
  });
  root.addEventListener('htmx:afterSwap', (event) => {
    const detail = detailOf(event);
    if (!pending || detail?.xhr !== pending.xhr) return;
    const memory = pending;
    pending = null;
    restore(memory);
  });
  // The control that survived the swap and was re-enabled when the request ended lost its focus when it was disabled.
  root.addEventListener('htmx:afterRequest', (event) => {
    const { xhr } = detailOf(event);
    inFlight.delete(xhr);
    const control = pressed.get(xhr);
    if (control?.isConnected && lostFocus() && !control.matches(':disabled')) focus(control);
  });
}
