/**
 * The fold state of the side menu, as a cookie (spec/components/side-menu.md §4). The server reads the same cookie
 * (backend/internal/web/navstate) and draws the menu folded from the first byte, so there is no flash of the open menu.
 * A cookie, not localStorage: the desktop app loads the loopback URL on a port that can change from run to run, and a cookie is
 * shared across ports where localStorage is not.
 */

/** A year in seconds: how long the choice is kept. */
export const MAX_AGE_SECONDS = 365 * 24 * 60 * 60;

/** The `document.cookie` string that stores the state: the folded value when folded, an empty one (open) when not. */
export function stateCookie(name: string, foldedValue: string, folded: boolean): string {
  return `${name}=${folded ? foldedValue : 'open'}; Path=/; Max-Age=${MAX_AGE_SECONDS}; SameSite=Strict`;
}
