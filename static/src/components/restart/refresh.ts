/**
 * Reads the "再起動して反映" panel again after a save (#1006, #1007).
 *
 * A save of any settings category can make a setting wait for a restart, so the panel at the top of the settings screen
 * (`#restart-slot`, which asks `GET /settings/restart`) must be read again after it. htmx evaluates no string
 * (`allowEval` is off), so the filter of "a request that writes, but not the panel's own" is here, not in an `hx-trigger`
 * filter. The slot listens for `restart-refresh`.
 */

/** The panel's own address: its buttons answer with what they did, which a re-read would replace. */
export const RESTART_PATH = '/settings/restart';

/** The event the slot's `hx-trigger` listens for. */
export const REFRESH_EVENT = 'restart-refresh';

/** Whether a finished request is one after which the panel is read again: a write, and not a request of the panel. */
export function shouldRefresh(verb: string | undefined, path: string | undefined): boolean {
  if (!verb || verb.toLowerCase() === 'get') return false;
  return path !== RESTART_PATH;
}

interface AfterRequestDetail {
  requestConfig?: { verb?: string };
  pathInfo?: { requestPath?: string };
  successful?: boolean;
}

export function installRestartRefresh(root: Document = document): void {
  root.addEventListener('htmx:afterRequest', (event) => {
    const detail = (event as CustomEvent<AfterRequestDetail>).detail;
    if (!detail.successful || !shouldRefresh(detail.requestConfig?.verb, detail.pathInfo?.requestPath)) return;
    const slot = root.getElementById('restart-slot');
    // After the swap of the save's own answer; the slot reads the panel itself.
    if (slot) setTimeout(() => slot.dispatchEvent(new CustomEvent(REFRESH_EVENT)), 300);
  });
}
