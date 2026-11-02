import { noRefNotice, unionRoles } from './inputs-model';

// What the model cards say about input images, and what the controls of the images, the options and the notice do about it.
// The cards carry data-accepts-references / data-reference-roles / data-supports-lora (models.templ); the controls are the ones
// refrow.templ and refopts.templ draw. A control that is hidden is disabled too, so that it posts nothing.

interface Chosen {
  label: string;
  roles: string[];
  lora: boolean;
}

/** The models that are ticked in the form. */
export function chosenModels(form: HTMLElement): Chosen[] {
  return [...form.querySelectorAll<HTMLInputElement>('[data-model] input[name="model"]:checked')].map((input) => {
    const card = input.closest<HTMLElement>('[data-model]')!;
    return {
      label: card.dataset.label ?? card.dataset.model ?? '',
      roles: (card.dataset.referenceRoles ?? '').split(' ').filter(Boolean),
      lora: card.dataset.supportsLora === 'true',
    };
  });
}

/** Shows or hides a control and disables what is inside it while it is hidden. */
export function toggle(el: HTMLElement | null, show: boolean): void {
  if (!el) return;
  el.hidden = !show;
  for (const field of el.querySelectorAll<HTMLInputElement | HTMLSelectElement>('input,select')) field.disabled = !show;
}

/** Offers only `roles` in a role select, and moves it to the first one offered when its own is not. */
function offerRoles(select: HTMLSelectElement, roles: readonly string[]): void {
  for (const option of select.options) {
    option.disabled = option.hidden = !roles.includes(option.value);
  }
  if (select.selectedOptions[0]?.disabled) {
    const first = [...select.options].find((o) => !o.disabled);
    if (first) select.value = first.value;
  }
}

/** Draws the controls for the models that are ticked now. */
export function applyModels(fieldset: HTMLElement, form: HTMLElement, refCount: number): void {
  if (fieldset.dataset.media === 'video') return;
  const chosen = chosenModels(form);
  const roles = unionRoles(chosen.map((m) => m.roles));
  for (const controls of fieldset.querySelectorAll<HTMLElement>('[data-gt-ref-controls]')) {
    toggle(controls, roles.length > 0);
    const select = controls.querySelector<HTMLSelectElement>('[data-gt-role]');
    if (select && roles.length > 0) offerRoles(select, roles);
  }
  toggle(fieldset.querySelector('[data-gt-avoid]'), roles.length > 0);
  toggle(
    fieldset.querySelector('[data-gt-lora]'),
    chosen.some((m) => m.lora),
  );
  const notice = fieldset.querySelector<HTMLElement>('[data-gt-noref]');
  if (notice) {
    notice.textContent = noRefNotice(
      refCount,
      chosen.map((m) => ({ label: m.label, accepts: m.roles.length > 0 })),
    );
    notice.hidden = notice.textContent === '';
  }
}
