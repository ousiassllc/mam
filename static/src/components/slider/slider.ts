/**
 * The common slider (#955): every `input[type=range]` of the app looks as slider.css draws it, and the part of the rail up to the
 * value is painted from `--fill`, which is written here: at load, in a swapped body, and as the value moves (drag, arrow keys).
 * A slider that a Lit component draws writes `--fill` itself (sliderStyle). The listener is on the document: sliders may arrive in
 * a swapped body.
 */

import { sliderFraction } from './slider-model';

const SLIDERS = 'input[type="range"]';

/** Writes the fill of every slider under root (and of root itself). */
export function syncSliders(root: ParentNode = document): void {
  const own = root instanceof HTMLInputElement && root.matches(SLIDERS) ? [root] : [];
  for (const el of [...own, ...root.querySelectorAll<HTMLInputElement>(SLIDERS)]) {
    el.style.setProperty('--fill', String(sliderFraction(el)));
  }
}

/** Installs the fill of the sliders. Call it once (main.ts). */
export function installSlider(): void {
  document.addEventListener('input', (event) => {
    const el = event.target;
    if (el instanceof HTMLInputElement && el.matches(SLIDERS)) syncSliders(el);
  });
  document.addEventListener('DOMContentLoaded', () => syncSliders());
  document.addEventListener('htmx:load', (event) => syncSliders(event.target as ParentNode));
}
