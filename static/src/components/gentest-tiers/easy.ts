/**
 * The easy tier of the generation test form (Issue #977). Choosing what to make swaps the part of the form that follows it (the model that
 * choice picks, the quality buttons: #gt-easy-pick); this keeps the rest of the form in step with it: the estimate button is off while no model was
 * picked, and the input images (inputs.ts) hear that the model they read changed. The listeners are on the document: the form may arrive in a
 * swapped body.
 */

const PICK = '#gt-easy-pick';

function sync(pick: HTMLElement): void {
  const form = pick.closest('form');
  const button = form?.querySelector<HTMLButtonElement>('[data-gt-easy-estimate]');
  if (button) button.disabled = pick.dataset.canEstimate !== 'true';
}

function syncAll(root: ParentNode): void {
  for (const pick of root.querySelectorAll<HTMLElement>(PICK)) sync(pick);
}

/** Installs the easy tier's behaviour. Call it once (main.ts). */
export function initGentestEasy(root: Document = document): void {
  root.addEventListener('htmx:afterSwap', (event) => {
    const el = event.target as HTMLElement | null;
    if (!el?.matches?.(PICK)) return;
    sync(el);
    // the model's card is new: the input images read what it takes
    el.querySelector('input[name="model"]')?.dispatchEvent(new Event('change', { bubbles: true }));
  });
  root.addEventListener('htmx:load', (event) => syncAll(event.target as ParentNode));
  root.addEventListener('DOMContentLoaded', () => syncAll(root));
}
