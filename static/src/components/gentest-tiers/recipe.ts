/**
 * The recipes of the generation test form (Issue #977; gentesttier/recipe.templ). The recipe section sits inside the compose form, so the Enter key in
 * the box of a name would send the form (the estimate): it presses the button that belongs to the box instead. The listeners are on the document: the
 * form may arrive in a swapped body.
 */

/** Installs the behaviour. Call it once (main.ts). */
export function initGentestRecipe(root: Document = document): void {
  root.addEventListener('keydown', (event) => {
    const el = event.target as HTMLElement | null;
    if (
      event.key !== 'Enter' ||
      event.isComposing ||
      !(el instanceof HTMLInputElement) ||
      !el.matches('[data-gt-recipe-name]')
    )
      return;
    event.preventDefault(); // not the estimate
    el.closest<HTMLElement>('[data-gt-recipe-row]')?.querySelector<HTMLButtonElement>('[data-gt-recipe-act]')?.click();
  });
}
