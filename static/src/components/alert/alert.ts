/**
 * Alerts (#alert-region, at the top of the page; the Ousiass UI guide, O3): what went wrong with an action (a LOCK violation, an exceeded budget, a
 * request that did not get through). An alert stays until the user closes it: its message may be long, and one that vanishes on a timer cannot be
 * read at the reader's own pace or by a screen reader (WCAG 2.2.1). The markup is defined once, in atoms/alert.templ; the page shell embeds it as
 * #alert-template and this module clones it.
 */
import { createLogger } from '../lib/logger';

const log = createLogger('alert');
const CLOSE = '[data-alert-close]';
const MESSAGE = '[data-alert-message]';

/** The kinds of alert the scripts raise: what went wrong (#alert-template) and what the user has to know about (#alert-warning-template). */
export type AlertKind = 'error' | 'warning';

const TEMPLATE_ID: Record<AlertKind, string> = { error: 'alert-template', warning: 'alert-warning-template' };

/** An alert of `kind` (an error by default) for `message`, not yet in the document; null when the page has no template for it. */
export function createAlert(message: string, kind: AlertKind = 'error'): HTMLElement | null {
  const template = document.getElementById(TEMPLATE_ID[kind]);
  const alert = template instanceof HTMLTemplateElement ? template.content.firstElementChild?.cloneNode(true) : null;
  if (!(alert instanceof HTMLElement)) {
    log.warn(`#${TEMPLATE_ID[kind]} is missing; cannot show`, message);
    return null;
  }
  const text = alert.querySelector(MESSAGE);
  if (text) text.textContent = message;
  return alert;
}

function messageOf(alert: Element): string {
  return alert.querySelector(MESSAGE)?.textContent ?? '';
}

/** Whether `region` already shows an alert with this message. */
export function hasAlert(region: Element, message: string): boolean {
  return Array.from(region.querySelectorAll('[data-alert]')).some((alert) => messageOf(alert) === message);
}

/**
 * Appends an alert (an error by default) for `message` unless the region already shows one with the same message: a failure that repeats (the 15s Cost Summary poll
 * while the server is down) must not pile up alerts that stay until closed one by one.
 */
export function appendAlert(region: Element | null, message: string, kind: AlertKind = 'error'): void {
  if (!region || hasAlert(region, message)) return;
  const alert = createAlert(message, kind);
  if (alert) region.append(alert);
}

/**
 * Drops, from an out-of-band response about to be swapped into #alert-region, the alerts that the region already shows (same rule as appendAlert, for
 * alerts the server sends).
 */
export function dedupeAlertsOnSwap(root: Document | HTMLElement = document): void {
  root.addEventListener('htmx:oobBeforeSwap', (event) => {
    const { target, fragment } = (event as CustomEvent<{ target?: Element; fragment?: ParentNode }>).detail;
    if (target?.id !== 'alert-region' || !fragment) return;
    const seen = new Set(Array.from(target.querySelectorAll('[data-alert]')).map(messageOf));
    for (const alert of Array.from(fragment.querySelectorAll('[data-alert]'))) {
      const message = messageOf(alert);
      if (seen.has(message)) alert.remove();
      else seen.add(message);
    }
  });
}

/** Makes the close button of any alert, present or added later, remove that alert. */
export function dismissAlertsOnClick(root: Document | HTMLElement = document): void {
  root.addEventListener('click', (event) => {
    const button = event.target instanceof Element ? event.target.closest(CLOSE) : null;
    button?.closest('[data-alert]')?.remove();
  });
}
