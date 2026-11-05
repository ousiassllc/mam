/**
 * The scrim closes (the Ousiass UI guide, O1): a modal <dialog> closes when the user clicks outside it, on its scrim (the `::backdrop`), as it does on
 * Esc. A click on the dialog's own padding is a click inside it. A dialog that holds what the user has typed (a form with a changed field) asks first
 * (the confirmation modal, O2), so that a stray click does not throw the input away.
 */
import { confirmDialog } from '../confirm/confirm';

const DIRTY = 'scrimDirty';
const LOST_INPUT = '入力した内容は保存されません。閉じますか？';

/** Whether a click at (`x`, `y`) is outside the box of the dialog: on its scrim. */
export function isOnScrim(box: Pick<DOMRect, 'left' | 'right' | 'top' | 'bottom'>, x: number, y: number): boolean {
  return x < box.left || x > box.right || y < box.top || y > box.bottom;
}

/** Installs the scrim click. Call it once (main.ts). */
export function installScrim(root: Document = document): void {
  // What the user typed into a dialog: it is cleared when the dialog closes (`close` does not bubble: capture phase).
  root.addEventListener('input', (event) => {
    const dialog = event.target instanceof Element ? event.target.closest('dialog') : null;
    if (dialog) dialog.dataset[DIRTY] = '';
  });
  root.addEventListener(
    'close',
    (event) => {
      if (event.target instanceof HTMLDialogElement) delete event.target.dataset[DIRTY];
    },
    true,
  );
  root.addEventListener('click', (event) => {
    const dialog = event.target;
    if (!(dialog instanceof HTMLDialogElement) || !dialog.open) return;
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
