/**
 * The theme button of the side menu (backend/internal/web/organisms/sidenav): a click on `[data-theme-toggle]` switches the page
 * between Light and Dark. The theme is the `data-theme` of <html> (static/src/css/tokens.css switches the colour tokens on it),
 * and the cookie that the server reads to draw the next page the same way. The button is named for what it does, not for the state, and
 * its icon is the same in both themes.
 *
 * The cookie name is the data-theme-cookie attribute of the button, so that the Go component is its only source.
 */
import { otherTheme, type Theme, themeCookie } from './theme-model';

/** The theme the page shows now: the chosen one, else the system's. */
function shownTheme(root: HTMLElement): Theme {
  const chosen = root.dataset.theme;
  if (chosen === 'light' || chosen === 'dark') return chosen;
  return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function toggle(button: HTMLElement): void {
  const root = document.documentElement;
  const next = otherTheme(shownTheme(root));
  root.dataset.theme = next;
  // biome-ignore lint/suspicious/noDocumentCookie: the Cookie Store API is async and not in every WebView of the desktop app; the server reads this cookie
  document.cookie = themeCookie(button.dataset.themeCookie ?? '', next);
}

/** Installs the theme button. Call it once (main.ts). */
export function installTheme(root: Document = document): void {
  root.addEventListener('click', (event) => {
    const button = (event.target as Element | null)?.closest<HTMLElement>('[data-theme-toggle]');
    if (button) toggle(button);
  });
}
