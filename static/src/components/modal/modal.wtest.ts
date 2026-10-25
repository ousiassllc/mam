import { expect, waitUntil } from '@open-wc/testing';
import { sendKeys } from '@web/test-runner-commands';
import htmx from 'htmx.org';
import '../../main';

const DATA = '/src/components/testdata';

describe('modal dialog (spec/components/shared-ui-parts.md §4)', () => {
  let host: HTMLElement;
  let opener: HTMLButtonElement;

  const dialog = () => host.querySelector<HTMLDialogElement>('dialog')!;
  const body = () => host.querySelector<HTMLElement>('#mam-modal-body')!;
  const byId = (id: string) => document.getElementById(id)!;

  function render(openUrl = `${DATA}/modal-edit.html`): void {
    host = document.createElement('div');
    host.innerHTML = `
      <ul id="rows"><li id="row-a">
        <button type="button" id="opener" data-modal-open data-focus-key="edit-a" hx-get="${openUrl}" hx-target="#mam-modal-body" hx-swap="innerHTML">設定</button>
      </li></ul>
      <button type="button" id="elsewhere">ほかのボタン</button>
      <dialog id="mam-modal" data-modal aria-labelledby="mam-modal-title">
        <div id="mam-modal-body"></div>
        <p id="mam-modal-failure" role="alert" hidden></p>
        <button type="button" data-modal-close aria-label="閉じる">×</button>
      </dialog>`;
    document.body.append(host);
    htmx.process(host);
    opener = byId('opener') as HTMLButtonElement;
  }

  async function open(): Promise<void> {
    opener.focus();
    opener.click();
    await waitUntil(() => dialog().open, 'the dialog did not open', { timeout: 2000 });
    // htmx processes the new content (the hx-* attributes of its form) when the swap settles.
    await waitUntil(() => !body().inert, 'the swap did not settle', { timeout: 2000 });
  }

  beforeEach(() => render());
  afterEach(() => {
    if (dialog().open) dialog().close();
    host.remove();
  });

  it('opens when the body arrives, names itself, and puts the focus on the autofocus control', async () => {
    expect(dialog().open).to.equal(false);
    await open();

    expect(document.activeElement === byId('modal-input')).to.equal(true);
    expect(dialog().getAttribute('aria-labelledby')).to.equal('mam-modal-title');
    expect(dialog().getAttribute('aria-describedby')).to.equal('mam-modal-desc');
    // modal: the rest of the page cannot take the focus
    byId('elsewhere').focus();
    expect(document.activeElement === byId('elsewhere')).to.equal(false);
  });

  it('keeps the new content from being used until htmx has processed it, so a form is never sent by the browser', async () => {
    // These listeners come after the modal's own, so they see what it has done when the event reaches them.
    const seen: { swapInert?: boolean; settleInert?: boolean; settleFocus?: boolean } = {};
    const onSwap = (e: Event) => {
      if ((e.target as Element).id === 'mam-modal-body') seen.swapInert = body().inert;
    };
    const onSettle = (e: Event) => {
      if ((e.target as Element).id !== 'mam-modal-body') return;
      seen.settleInert = body().inert;
      seen.settleFocus = document.activeElement === byId('modal-input');
    };
    document.addEventListener('htmx:afterSwap', onSwap);
    document.addEventListener('htmx:afterSettle', onSettle);
    try {
      await open();
    } finally {
      document.removeEventListener('htmx:afterSwap', onSwap);
      document.removeEventListener('htmx:afterSettle', onSettle);
    }

    // inert after the swap (not yet processed), usable and focused once settled
    expect(seen.swapInert).to.equal(true);
    expect(seen.settleInert).to.equal(false);
    expect(seen.settleFocus).to.equal(true);
  });

  it('closes on Esc, empties the body and gives the focus back to the button that opened it', async () => {
    await open();

    await sendKeys({ press: 'Escape' });
    await waitUntil(() => !dialog().open, 'Esc did not close the dialog', { timeout: 2000 });

    expect(body().children.length).to.equal(0);
    expect(document.activeElement === opener).to.equal(true);
  });

  it('closes on the cancel button and on the close button, and does not close on the backdrop', async () => {
    await open();
    const box = dialog().getBoundingClientRect();
    dialog().dispatchEvent(new MouseEvent('click', { bubbles: true, clientX: box.left - 5, clientY: box.top - 5 }));
    expect(dialog().open).to.equal(true);

    (host.querySelector('#mam-modal-body [data-modal-close]') as HTMLButtonElement).click();
    await waitUntil(() => !dialog().open, 'the cancel button did not close', { timeout: 2000 });
    expect(document.activeElement === opener).to.equal(true);

    await open();
    (host.querySelector('dialog > [data-modal-close]') as HTMLButtonElement).click();
    await waitUntil(() => !dialog().open, 'the close button did not close', { timeout: 2000 });
  });

  it('closes after a save, swaps the row out of band and moves the focus to the same button of the new row', async () => {
    await open();
    const oldOpener = opener;

    (byId('modal-save') as HTMLButtonElement).click();
    await waitUntil(() => !dialog().open, 'a saved form did not close the dialog', { timeout: 2000 });

    expect(byId('row-a').textContent).to.contain('保存済み');
    expect(byId('opener') !== oldOpener).to.equal(true);
    expect(oldOpener.isConnected).to.equal(false);
    expect(document.activeElement === byId('opener')).to.equal(true);
    expect(body().children.length).to.equal(0);
  });

  it('keeps the dialog open on 422, shows the reason, keeps what was typed and focuses the field to blame', async () => {
    await open();

    (byId('modal-invalid') as HTMLButtonElement).click();
    await waitUntil(() => document.getElementById('mam-modal-error') !== null, 'the 422 body was not swapped in', {
      timeout: 2000,
    });

    expect(dialog().open).to.equal(true);
    expect(byId('mam-modal-error').textContent).to.contain('入力が正しくありません');
    expect((byId('modal-input') as HTMLInputElement).value).to.equal('typed');
    expect(document.activeElement === byId('modal-input')).to.equal(true);
  });

  it('opens with the reason and a close button when the target is gone (404 / 409)', async () => {
    host.remove();
    render(`${DATA}/modal-notice.html`);
    await open();

    expect(byId('mam-modal-error').textContent).to.contain('見つかりません');
    expect(document.activeElement === host.querySelector('#mam-modal-body [data-modal-close]')).to.equal(true);
  });

  it('says a failed request inside the dialog in the dialog (the toast region is behind it)', async () => {
    await open();

    (byId('modal-broken') as HTMLButtonElement).click();
    const failure = byId('mam-modal-failure');
    await waitUntil(() => !failure.hidden, 'the failure was not shown in the dialog', { timeout: 2000 });

    expect(failure.textContent).to.contain('404');
    expect(dialog().open).to.equal(true);
    expect(document.querySelector('#toast-region [data-toast]')).to.equal(null);
  });

  it('is closed and emptied before the page goes into the history, so no snapshot restores it open', async () => {
    await open();
    expect(body().innerHTML).to.contain('modal-input');

    document.dispatchEvent(new CustomEvent('htmx:beforeHistorySave'));

    expect(dialog().open).to.equal(false);
    expect(dialog().outerHTML).to.not.contain(' open');
    expect(body().children.length).to.equal(0);
  });
});
