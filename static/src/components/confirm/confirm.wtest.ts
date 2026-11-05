import { expect, waitUntil } from '@open-wc/testing';
import { confirmDialog } from './confirm';

const CONFIRM =
  '<dialog data-confirm><p data-confirm-message></p><button type="button" data-confirm-cancel>キャンセル</button><button type="button" data-confirm-ok>実行する</button></dialog>';

describe('confirmDialog', () => {
  let template: HTMLTemplateElement;
  beforeEach(() => {
    template = document.createElement('template');
    template.id = 'confirm-template';
    template.innerHTML = CONFIRM;
    document.body.append(template);
  });
  afterEach(() => {
    template.remove();
    document.querySelectorAll('dialog[data-confirm]').forEach((d) => {
      d.remove();
    });
  });

  const open = () => document.querySelector('dialog[data-confirm][open]') as HTMLDialogElement | null;

  it('opens a modal with the question and answers true on the button that goes on', async () => {
    const answer = confirmDialog('削除します。よろしいですか？');
    await waitUntil(open, 'the question was not asked');
    expect(open()!.querySelector('[data-confirm-message]')!.textContent).to.equal('削除します。よろしいですか？');
    (open()!.querySelector('[data-confirm-ok]') as HTMLButtonElement).click();
    expect(await answer).to.equal(true);
    expect(document.querySelector('dialog[data-confirm]')).to.equal(null);
  });

  it('answers false on キャンセル and on Esc (the native cancel)', async () => {
    let answer = confirmDialog('q');
    await waitUntil(open);
    (open()!.querySelector('[data-confirm-cancel]') as HTMLButtonElement).click();
    expect(await answer).to.equal(false);

    answer = confirmDialog('q');
    await waitUntil(open);
    open()!.close();
    expect(await answer).to.equal(false);
  });

  it('does not go on when the page has no template', async () => {
    template.remove();
    expect(await confirmDialog('q')).to.equal(false);
  });

  it('holds the request of an hx-confirm element until the question is answered yes', async () => {
    const { installConfirm } = await import('./confirm');
    const root = document.createElement('div');
    document.body.append(root);
    installConfirm(root as unknown as Document);
    let issued = 0;
    const event = new CustomEvent('htmx:confirm', {
      bubbles: true,
      cancelable: true,
      detail: { question: '削除しますか？', issueRequest: () => (issued += 1) },
    });
    expect(root.dispatchEvent(event)).to.equal(false);
    await waitUntil(open);
    expect(issued).to.equal(0);
    (open()!.querySelector('[data-confirm-ok]') as HTMLButtonElement).click();
    await waitUntil(() => issued === 1);

    const noQuestion = new CustomEvent('htmx:confirm', {
      bubbles: true,
      cancelable: true,
      detail: { question: null, issueRequest: () => {} },
    });
    expect(root.dispatchEvent(noQuestion)).to.equal(true);
    root.remove();
  });
});
