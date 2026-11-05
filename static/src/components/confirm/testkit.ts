/**
 * Answers the confirmation modal (components/confirm) by itself and records the questions: the page gets the template that AppLayout embeds, and
 * a dialog that is opened is answered at once with the button for `answer`.
 */
export function stubConfirm(answer: boolean) {
  const messages: string[] = [];
  const template = document.createElement('template');
  template.id = 'confirm-template';
  template.innerHTML =
    '<dialog data-confirm><p data-confirm-message></p><button type="button" data-confirm-cancel>キャンセル</button><button type="button" data-confirm-ok>実行する</button></dialog>';
  document.body.append(template);
  const observer = new MutationObserver(() => {
    for (const dialog of document.querySelectorAll<HTMLDialogElement>('dialog[data-confirm]')) {
      if (!dialog.open || dialog.dataset.answered) continue;
      dialog.dataset.answered = 'yes';
      messages.push(dialog.querySelector('[data-confirm-message]')?.textContent ?? '');
      dialog.querySelector<HTMLButtonElement>(answer ? '[data-confirm-ok]' : '[data-confirm-cancel]')?.click();
    }
  });
  observer.observe(document.body, { childList: true });
  return {
    messages,
    restore: () => {
      observer.disconnect();
      template.remove();
    },
  };
}
