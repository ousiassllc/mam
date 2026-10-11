/**
 * htmx stores the history element's innerHTML in sessionStorage. Markers and nodes that exist
 * only because JavaScript already ran must not be in that snapshot: Back restores new DOM, and
 * those markers would make init skip it while the listeners and sockets were left behind.
 *
 * `htmx:beforeHistorySave` hands over the live element, and htmx clones it only after the event
 * returns, so removing nodes there would also remove them from the page still on screen.
 * `htmx:historyItemCreated` carries the cloned HTML before it is cached; that string is what we clean.
 * Lit components render into the light DOM, so a snapshot also contains that paint. Restoring it and
 * letting Lit render again would leave the old nodes in front of the new ones.
 */

const READY = ['data-shot-ready', 'data-chat-ready'] as const;

/** Strips init markers, the connection notice and the toasts on screen from an htmx history snapshot. */
export function sanitizeHistoryHtml(html: string): string {
  const template = document.createElement('template');
  template.innerHTML = html;
  const root = template.content;
  root.querySelector('#connection-lost-region')?.remove();
  // Toasts stay until closed, so one still open would come back with the restored page.
  for (const toast of root.querySelectorAll('#toast-region [data-toast]')) toast.remove();
  for (const name of READY) {
    for (const el of root.querySelectorAll(`[${name}]`)) el.removeAttribute(name);
  }
  // Attributes stay, so the component can render again; the previous paint does not.
  for (const el of root.querySelectorAll('*')) {
    if (el.localName.startsWith('mam-')) el.replaceChildren();
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
