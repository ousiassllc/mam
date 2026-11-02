/**
 * The fold button of the side menu (backend/internal/web/organisms/sidenav, spec/components/side-menu.md §4): a click on
 * `[data-sidenav-toggle]` folds the menu to its icons or opens it again. The state is the `data-folded` of the menu (the CSS
 * hides the labels and swaps the icon from it), the `aria-expanded` and the name of the button, and the cookie that the server
 * reads to draw the next page the same way.
 *
 * The cookie name, its folded value and the two names of the button are data attributes of the menu, so that the Go
 * component is their only source.
 */
import { stateCookie } from './sidenav-model';

function toggle(nav: HTMLElement, button: HTMLElement): void {
  const folded = nav.dataset.folded !== 'true';
  nav.dataset.folded = String(folded);
  const label = (folded ? nav.dataset.labelOpen : nav.dataset.labelFold) ?? '';
  button.setAttribute('aria-expanded', String(!folded));
  button.setAttribute('aria-label', label);
  button.setAttribute('title', label);
  // biome-ignore lint/suspicious/noDocumentCookie: the Cookie Store API is async and not in every WebView of the desktop app; the server reads this cookie
  document.cookie = stateCookie(nav.dataset.cookie ?? '', nav.dataset.cookieFolded ?? 'folded', folded);
}

/** Installs the fold button. Call it once (main.ts). */
export function installSidenav(root: Document = document): void {
  root.addEventListener('click', (event) => {
    const button = (event.target as Element | null)?.closest<HTMLElement>('[data-sidenav-toggle]');
    const nav = button?.closest<HTMLElement>('nav[data-folded]');
    if (button && nav) toggle(nav, button);
  });
}
