/**
 * The colour theme of the page, as a cookie (backend/internal/web/themestate reads the same one and draws `<html data-theme>` from
 * the first byte). A cookie, not localStorage: the desktop app loads the loopback URL on a port that can change from run to run.
 */

/** A year in seconds: how long the choice is kept. */
export const MAX_AGE_SECONDS = 365 * 24 * 60 * 60;

export type Theme = 'light' | 'dark';

/** The `document.cookie` string that stores the chosen theme. */
export function themeCookie(name: string, theme: Theme): string {
  return `${name}=${theme}; Path=/; Max-Age=${MAX_AGE_SECONDS}; SameSite=Strict`;
}

/** The theme the button switches to: the other one than the page shows now (the chosen one, else the system's). */
export function otherTheme(shown: Theme): Theme {
  return shown === 'dark' ? 'light' : 'dark';
}
