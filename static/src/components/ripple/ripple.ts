/**
 * The ripple of a press on a button or a close button (the Ousiass UI guide, M4): a click on an element with `data-ripple`
 * (atoms.Button and its kin, the close buttons) starts a circle at the pressed point that grows over the button and fades out in
 * --motion-slow with --ease-out, then goes. A disabled button gives none, and a reader who asked for reduced motion gets none (M3).
 * The circle takes the colour of the text (`currentColor`) at a low opacity, so it follows the theme.
 */
import { milliseconds, rippleAt } from './ripple-model';

const SELECTOR = '[data-ripple]';

function duration(): number {
  const slow = milliseconds(getComputedStyle(document.documentElement).getPropertyValue('--motion-slow'));
  return slow > 0 ? slow : 280;
}

function easing(): string {
  return getComputedStyle(document.documentElement).getPropertyValue('--ease-out').trim() || 'ease-out';
}

function start(button: HTMLElement, event: PointerEvent): void {
  const box = button.getBoundingClientRect();
  const { left, top, size } = rippleAt(event.clientX - box.left, event.clientY - box.top, box.width, box.height);
  const wave = document.createElement('span');
  wave.setAttribute('aria-hidden', 'true');
  wave.style.cssText = `position:absolute;pointer-events:none;border-radius:9999px;background:currentColor;opacity:0;left:${left}px;top:${top}px;width:${size}px;height:${size}px`;
  button.append(wave);
  const run = wave.animate(
    [
      { transform: 'scale(0)', opacity: 0.2 },
      { transform: 'scale(1)', opacity: 0 },
    ],
    { duration: duration(), easing: easing() },
  );
  run.finished.then(
    () => wave.remove(),
    () => wave.remove(),
  );
}

/** Installs the ripple. Call it once (main.ts). */
export function installRipple(root: Document = document): void {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)');
  root.addEventListener('pointerdown', (event) => {
    if (reduced.matches || event.button !== 0) return;
    const button = (event.target as Element | null)?.closest<HTMLElement>(SELECTOR);
    if (!button || button.matches(':disabled, [aria-disabled="true"]')) return;
    start(button, event);
  });
}
