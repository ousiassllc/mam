/**
 * The fade-out of the overlays (the Ousiass UI guide, M5): what is taken out of the page (a confirmation modal, an alert) or emptied (the body of
 * a modal that was closed) is let go only when the CSS transition of its exit (static/src/css/motion.css: `--motion-fade-out`, `--ease-in`; at most
 * 100ms under `prefers-reduced-motion`) is over, so that the fade is seen. The time is the transition's own, not a timer of ours.
 */

/**
 * Resolves when the CSS transitions that `el` (and its pseudo-elements, the scrim of a dialog) is running have finished. It resolves at once
 * when none is running (no motion, or a browser without `allow-discrete`). A transition that is cancelled (the element left the document) ends the
 * wait too. Only transitions are waited for: a spinner inside that never ends must not hold the exit.
 */
export async function whenFaded(el: Element): Promise<void> {
  // getAnimations() brings the style up to date first, so the transition that the change just started is already in the list.
  const transitions = el.getAnimations({ subtree: true }).filter((animation) => animation instanceof CSSTransition);
  await Promise.allSettled(transitions.map((transition) => transition.finished));
}
