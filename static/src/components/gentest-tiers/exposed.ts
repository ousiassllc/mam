/**
 * The exposed inputs of the generation test form (Issue #977: the "画面に出す値" a ComfyUI workflow offers as fields of their own, drawn by
 * organisms/gentestadv/exposed.templ). Each model that has some has a group of fields; a group is shown while its model is ticked and its fields are
 * disabled while it is hidden, so that it posts nothing. The server draws the groups of the models that are ticked as shown, so this only follows the
 * ticking. The listeners are on the document: the form may arrive in a swapped body.
 */

const GROUP = '[data-gt-exposed-model]';

/** The ids of the models that are ticked in the form (one that the commercial filter hid is disabled and does not count). */
export function tickedModels(form: ParentNode): Set<string> {
  return new Set(
    [...form.querySelectorAll<HTMLInputElement>('input[name="model"]:checked:not(:disabled)')].map(
      (input) => input.value,
    ),
  );
}

/** Shows the groups of the ticked models and hides (and disables the fields of) the others. */
export function syncExposed(form: ParentNode): void {
  const ticked = tickedModels(form);
  for (const group of form.querySelectorAll<HTMLElement>(GROUP)) {
    const show = ticked.has(group.dataset.gtExposedModel ?? '');
    group.hidden = !show;
    for (const field of group.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>('[name]')) {
      field.disabled = !show;
    }
  }
}

function formOf(el: Element | null): ParentNode | null {
  return el?.closest('form') ?? null;
}

/** Installs the behaviour. Call it once (main.ts). */
export function initGentestExposed(root: Document = document): void {
  root.addEventListener('change', (event) => {
    const el = event.target as HTMLElement | null;
    if (el instanceof HTMLInputElement && el.name === 'model') {
      const form = formOf(el);
      if (form) syncExposed(form);
    }
  });
  root.addEventListener('commercial-filter-applied', () => {
    for (const group of root.querySelectorAll(GROUP)) {
      const form = formOf(group);
      if (form) syncExposed(form);
    }
  });
  root.addEventListener('htmx:load', (event) => {
    const el = event.target as HTMLElement;
    for (const group of el.querySelectorAll?.(GROUP) ?? []) {
      const form = formOf(group);
      if (form) syncExposed(form);
    }
  });
}
