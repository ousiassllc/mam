/**
 * htmx stores the history element's innerHTML in sessionStorage. Markers and nodes that exist
 * only because JavaScript already ran must not be in that snapshot: Back restores new DOM, and
 * those markers would make init skip it while the listeners and sockets were left behind.
 *
 * `htmx:beforeHistorySave` hands over the live element, and htmx clones it only after the event
 * returns, so removing nodes there would also remove them from the page still on screen.
 * `htmx:historyItemCreated` carries the cloned HTML before it is cached; that string is what we clean.
 */

const READY = ['data-shot-ready', 'data-chat-ready'] as const;

/** Strips init markers and the connection notice from an htmx history snapshot. */
export function sanitizeHistoryHtml(html: string): string {
  const template = document.createElement('template');
  template.innerHTML = html;
  const root = template.content;
  root.querySelector('#connection-lost-region')?.remove();
  for (const name of READY) {
    for (const el of root.querySelectorAll(`[${name}]`)) el.removeAttribute(name);
  }
  return template.innerHTML;
}

let installed = false;

/** Keeps the live page intact and cleans only the snapshot htmx is about to cache. */
export function installHistorySnapshotHygiene(): void {
  if (installed) return;
  installed = true;
  document.addEventListener('htmx:historyItemCreated', (event) => {
    const item = (event as CustomEvent<{ item?: { content?: string } }>).detail?.item;
    if (!item || typeof item.content !== 'string') return;
    item.content = sanitizeHistoryHtml(item.content);
  });
}
