/**
 * Controls that save as soon as they change (`hx-trigger="change"`: MODE, Production Template,
 * Quality, Confirmation Level). The browser moves the control to the chosen value before the
 * request is sent, so a failed save (server down, CSRF 403, 5xx) would leave the screen showing a
 * value that was never stored. These helpers put the control back to the last stored value: the
 * one it was drawn with, or the one the last successful save committed.
 */

type SaveOnChange = HTMLFormElement | HTMLSelectElement;

/** The element that sends the request, when it is a form or select that saves on change. */
export function isSaveOnChange(elt: unknown): elt is SaveOnChange {
  return (
    (elt instanceof HTMLFormElement || elt instanceof HTMLSelectElement) &&
    elt.getAttribute('hx-trigger')?.trim() === 'change'
  );
}

/**
 * Settles a control after its request ended. `status` is the HTTP status of the response (0 when
 * there was none: server down, timeout, abort). htmx's own `successful` flag cannot be used: main.ts
 * makes it swap HTML error responses, which clears the error flag of a 422 or 5xx.
 */
export function settleSavedChange(elt: unknown, status: number): void {
  if (status >= 200 && status < 400) commitSavedChange(elt);
  else revertUnsavedChange(elt);
}

/** Puts a control that failed to save back to the stored value. */
export function revertUnsavedChange(elt: unknown): void {
  if (!isSaveOnChange(elt)) return;
  if (elt instanceof HTMLFormElement) {
    elt.reset();
    return;
  }
  for (const option of elt.options) option.selected = option.defaultSelected;
}

/**
 * Makes the current value of a control the stored one. A response that swaps the control in draws
 * it fresh; one that swaps nothing (the Quality select answers 204) would otherwise let a later
 * failure revert to the value the page was first drawn with.
 */
export function commitSavedChange(elt: unknown): void {
  if (!isSaveOnChange(elt)) return;
  if (elt instanceof HTMLSelectElement) {
    commitSelect(elt);
    return;
  }
  for (const control of elt.elements) {
    if (control instanceof HTMLSelectElement) commitSelect(control);
    else if (control instanceof HTMLInputElement) {
      control.defaultChecked = control.checked;
      control.defaultValue = control.value;
    }
  }
}

function commitSelect(select: HTMLSelectElement): void {
  for (const option of select.options) option.defaultSelected = option.selected;
}

/**
 * Settles every save-on-change control when its request ends (htmx fires `htmx:afterRequest` for a
 * response of any status and for a request that got none).
 */
export function installSaveOnChangeRevert(target: EventTarget = document): void {
  target.addEventListener('htmx:afterRequest', (event) => {
    const { elt, xhr } = (event as CustomEvent<{ elt: unknown; xhr: XMLHttpRequest }>).detail;
    settleSavedChange(elt, xhr.status);
  });
}
