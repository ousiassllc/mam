import { type Facts, factsOf, modelWeightOf } from './weight-facts';

const CHOSEN = '[data-model] input[name="model"]:checked:not(:disabled)';

/** How the chosen models of the form read weights: from the cards of the form (the easy tier's hidden one included). */
export function chosenFacts(form: HTMLFormElement | null): Facts {
  const cards = [...(form?.querySelectorAll<HTMLInputElement>(CHOSEN) ?? [])];
  return factsOf(cards.map((input) => modelWeightOf(input.closest<HTMLElement>('[data-model]')?.dataset ?? {})));
}

/** Calls `onChange` whenever the choice of models may have changed (a card ticked, a pick applied, the part of the easy tier's model drawn again); returns what stops it. */
export function watchChosen(form: HTMLFormElement | null, onChange: () => void): () => void {
  form?.addEventListener('change', onChange);
  form?.addEventListener('htmx:afterSettle', onChange);
  document.addEventListener('model-pick-applied', onChange);
  return () => {
    form?.removeEventListener('change', onChange);
    form?.removeEventListener('htmx:afterSettle', onChange);
    document.removeEventListener('model-pick-applied', onChange);
  };
}
