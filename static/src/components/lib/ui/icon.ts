import { html, svg, type TemplateResult } from 'lit';

/**
 * The icons the Lit widgets draw, with the paths of `atoms.Icon` (atoms/icon.templ, icon_player.templ: Iconoir v7.12.2, MIT, the
 * licence text is atoms/icons/LICENSE). The shape of the <svg> is the one of `iconSVG`: a decoration (aria-hidden), in the colour of
 * the text (currentColor), stroke 1.5. A button that holds one keeps its `aria-label`: the icon is not a name. To add one, copy the
 * path of `atoms.Icon` and the name of the case.
 */
export const PATHS = {
  play: [
    'M6.90588 4.53682C6.50592 4.2998 6 4.58808 6 5.05299V18.947C6 19.4119 6.50592 19.7002 6.90588 19.4632L18.629 12.5162C19.0211 12.2838 19.0211 11.7162 18.629 11.4838L6.90588 4.53682Z',
  ],
  pause: [
    'M6 18.4V5.6C6 5.26863 6.26863 5 6.6 5H9.4C9.73137 5 10 5.26863 10 5.6V18.4C10 18.7314 9.73137 19 9.4 19H6.6C6.26863 19 6 18.7314 6 18.4Z',
    'M14 18.4V5.6C14 5.26863 14.2686 5 14.6 5H17.4C17.7314 5 18 5.26863 18 5.6V18.4C18 18.7314 17.7314 19 17.4 19H14.6C14.2686 19 14 18.7314 14 18.4Z',
  ],
  'skip-prev': [
    'M6 7V17',
    'M17.0282 5.2672C17.4217 4.95657 18 5.23682 18 5.73813V18.2619C18 18.7632 17.4217 19.0434 17.0282 18.7328L9.09651 12.4709C8.79223 12.2307 8.79223 11.7693 9.09651 11.5291L17.0282 5.2672Z',
  ],
  'skip-next': [
    'M18 7V17',
    'M6.97179 5.2672C6.57832 4.95657 6 5.23682 6 5.73813V18.2619C6 18.7632 6.57832 19.0434 6.97179 18.7328L14.9035 12.4709C15.2078 12.2307 15.2078 11.7693 14.9035 11.5291L6.97179 5.2672Z',
  ],
  'arrow-up': ['M12 21L12 3M12 3L20.5 11.5M12 3L3.5 11.5'],
  'arrow-down': ['M12 3L12 21M12 21L20.5 12.5M12 21L3.5 12.5'],
  close: [
    'M6.75827 17.2426L12.0009 12M17.2435 6.75736L12.0009 12M12.0009 12L6.75827 6.75736M12.0009 12L17.2435 17.2426',
  ],
  warning: [
    'M20.0429 21H3.95705C2.41902 21 1.45658 19.3364 2.22324 18.0031L10.2662 4.01533C11.0352 2.67792 12.9648 2.67791 13.7338 4.01532L21.7768 18.0031C22.5434 19.3364 21.581 21 20.0429 21Z',
    'M12 9V13',
    'M12 17.01L12.01 16.9989',
  ],
} as const;

export type IconName = keyof typeof PATHS;

/** A decorative icon of `name`; `size` is the Tailwind size classes (size-icon-sm by default, 16px, as in the buttons of the widgets). */
export function icon(name: IconName, size = 'size-icon-sm'): TemplateResult {
  return html`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false" class=${size}>${PATHS[name].map((d) => svg`<path d=${d}></path>`)}</svg>`;
}
