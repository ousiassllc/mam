import { startingRoles } from './starting-roles';

// The input images of a video form (#1009): the first frame as the starting role while an I2V model is ticked, and the notice that the server draws
// (Service.CheckInputs, the check of the estimate) asked again when something it reads changes. The notice (inputnotice.CheckBox) listens for
// gt-inputs-changed on the form.

const lastSignature = new WeakMap<HTMLElement, string>();

/**
 * Draws a video form for the models that are ticked now (`labels` of the models, `needsFirstFrame` when one of them cannot run without a first frame):
 * the pictures the user gave no role take their starting role (data-role-touched marks the roles the user chose, which stay), and the notice is asked
 * again when the models or the roles changed.
 */
export function applyVideoInputs(
  fieldset: HTMLElement,
  form: HTMLElement,
  labels: readonly string[],
  needsFirstFrame: boolean,
): void {
  const selects = [...fieldset.querySelectorAll<HTMLSelectElement>('[data-gt-ref] [data-gt-role]')];
  const roles = startingRoles(
    selects.map((s) => ({ touched: s.dataset.roleTouched !== undefined, role: s.value })),
    needsFirstFrame,
  );
  for (const [i, select] of selects.entries()) select.value = roles[i] ?? select.value;
  const signature = JSON.stringify([labels, roles]);
  const before = lastSignature.get(fieldset);
  lastSignature.set(fieldset, signature);
  if (before !== undefined && before !== signature) form.dispatchEvent(new CustomEvent('gt-inputs-changed'));
}
