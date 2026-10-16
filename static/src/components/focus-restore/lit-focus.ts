/**
 * The focus rule of the Lit components (spec/components/overview.md §5.2): the control the user pressed is
 * disabled, re-rendered or removed by what the press did, and the browser drops the focus to <body>. Once
 * the component has rendered the result, the focus goes to the control that took its place, and failing that to
 * a region of the component (`tabindex="-1"`) so that a reader goes on from where the action was.
 */
import type { LitElement } from 'lit';

const FOCUSABLE = 'a[href],button,input:not([type="hidden"]),select,textarea,summary,[tabindex]';

/**
 * The focus is lost when it is on <body> or on a control that was disabled: newer Chrome (155) keeps a just-disabled
 * button as `document.activeElement` until a later frame, and only then drops the focus to <body>.
 */
export function lostFocus(): boolean {
  const current = document.activeElement;
  return !current || current === document.body || current.matches(':disabled');
}

/**
 * Focuses the first of `targets` that can take it (a disabled control cannot). An element that is not a control
 * (a row, a message) gets `tabindex="-1"` first: reachable by `focus()`, not by Tab.
 */
export function focusFirst(...targets: (Element | null | undefined)[]): boolean {
  for (const el of targets) {
    if (!(el instanceof HTMLElement) || !el.isConnected || el.matches(':disabled')) continue;
    if (!el.matches(FOCUSABLE)) el.setAttribute('tabindex', '-1');
    el.focus({ preventScroll: true });
    if (document.activeElement === el) return true;
  }
  return false;
}

/**
 * After `host` has rendered what the action changed, focuses the first of `pick()` when the focus was lost (the
 * pressed control was disabled, removed or moved in the DOM). A user who moved the focus elsewhere meanwhile keeps it.
 */
export async function focusAfterUpdate(host: LitElement, pick: () => (Element | null | undefined)[]): Promise<void> {
  await host.updateComplete;
  if (lostFocus()) focusFirst(...pick());
}
