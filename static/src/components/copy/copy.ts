/**
 * The "コピー" button of a value that is shown once (the MCP token that was just issued,
 * spec/components/settings-screen-categories.md §4): a button with `data-copy-source="<id of the field>"` copies the
 * value of that field to the clipboard and says so in the element named by `data-copy-status` (a `role="status"` line).
 *
 * The error box (#1010) copies a readonly textarea the same way and may name its own "copied" text in `data-copy-done`
 * (the page language); a button without it says the Japanese one.
 *
 * The clipboard is not always there (an insecure origin, a refused permission). Then the field is selected, so that the
 * value can be copied by hand, and the line says to do so. The value is never kept anywhere but the field.
 */

const COPIED = 'コピーしました';
const MANUAL = 'コピーできませんでした。選択した値を手動でコピーしてください';

function say(status: HTMLElement | null, text: string): void {
  if (status) status.textContent = text;
}

async function copy(button: HTMLElement): Promise<void> {
  const field = document.getElementById(button.getAttribute('data-copy-source') ?? '') as
    | HTMLInputElement
    | HTMLTextAreaElement
    | null;
  const status = document.getElementById(button.getAttribute('data-copy-status') ?? '');
  if (!field) return;
  try {
    await navigator.clipboard.writeText(field.value);
    say(status, button.getAttribute('data-copy-done') || COPIED);
  } catch {
    field.focus();
    field.select();
    say(status, MANUAL);
  }
}

/** Installs the copy buttons. Call it once (main.ts); the buttons may arrive later, in a swapped body. */
export function installCopy(root: Document = document): void {
  root.addEventListener('click', (event) => {
    const button = (event.target as Element | null)?.closest<HTMLElement>('[data-copy-source]');
    if (button) void copy(button);
  });
}
