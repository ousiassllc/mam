/**
 * The scrim closes (the Ousiass UI guide, O1): a modal <dialog> closes when the user clicks outside it, on its scrim (the `::backdrop`), as it does on
 * Esc. A click on the dialog's own padding is a click inside it. A press that starts inside the dialog and is let go on the scrim (selecting the text of
 * a field and dragging past the edge) is no click on the scrim, though the browser fires its click on the dialog (the common ancestor): the press must
 * have started on the scrim too. A dialog that holds what the user has typed into a form that it saves (a field of a
 * <form> inside the dialog, changed) asks first (the confirmation modal, O2), so that a stray click does not throw the input away. What is only a way of
 * choosing is not input: the search and the filters of a picker (their fields name no form, or a search box, or a form that asks with GET), and a
 * picker that sits in the page's own form (the form is outside the dialog) never ask; a click on the scrim closes them.
 */
import { confirmDialog } from '../confirm/confirm';

const DIRTY = 'scrimDirty';
const LOST_INPUT = '入力した内容は保存されません。閉じますか？';

/** Whether a click at (`x`, `y`) is outside the box of the dialog: on its scrim. */
export function isOnScrim(box: Pick<DOMRect, 'left' | 'right' | 'top' | 'bottom'>, x: number, y: number): boolean {
  return x < box.left || x > box.right || y < box.top || y > box.bottom;
}

/** Whether `field` is something the user typed for a form that the `dialog` saves, and that is lost when the dialog closes. */
function isSavedInput(field: Element, dialog: HTMLDialogElement): boolean {
  if (
    !(field instanceof HTMLInputElement || field instanceof HTMLSelectElement || field instanceof HTMLTextAreaElement)
  )
    return false;
  if (field instanceof HTMLInputElement && field.type === 'search') return false;
  const form = field.form; // null for a field that names a form that does not exist (`form="model-pick-no-form"`)
  return form !== null && dialog.contains(form) && !form.hasAttribute('hx-get');
}

/** Installs the scrim click. Call it once (main.ts). */
export function installScrim(root: Document = document): void {
  // Whether the press that ends in the next click started on a scrim; null while there is none (a click that no press came before, as a script's).
  let pressedOnScrim: boolean | null = null;
  root.addEventListener(
    'pointerdown',
    (event) => {
      const dialog = event.target;
      pressedOnScrim =
        dialog instanceof HTMLDialogElement &&
        dialog.open &&
        isOnScrim(dialog.getBoundingClientRect(), event.clientX, event.clientY);
    },
    true,
  );
  // What the user typed into a dialog: it is cleared when the dialog closes (`close` does not bubble: capture phase).
  root.addEventListener('input', (event) => {
    const field = event.target instanceof Element ? event.target : null;
    const dialog = field?.closest('dialog');
    if (field && dialog && isSavedInput(field, dialog)) dialog.dataset[DIRTY] = '';
  });
  root.addEventListener(
    'close',
    (event) => {
      if (event.target instanceof HTMLDialogElement) delete event.target.dataset[DIRTY];
    },
    true,
  );
  root.addEventListener('click', (event) => {
    const pressed = pressedOnScrim;
    pressedOnScrim = null;
    const dialog = event.target;
    if (!(dialog instanceof HTMLDialogElement) || !dialog.open) return;
    if (pressed === false) return;
    if (!isOnScrim(dialog.getBoundingClientRect(), event.clientX, event.clientY)) return;
    if (!(DIRTY in dialog.dataset)) {
      dialog.close();
      return;
    }
    void confirmDialog(LOST_INPUT).then((yes) => {
      if (yes && dialog.open) dialog.close();
    });
  });
}
