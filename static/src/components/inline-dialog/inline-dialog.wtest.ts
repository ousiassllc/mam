import { expect, waitUntil } from '@open-wc/testing';
import { sendKeys } from '@web/test-runner-commands';
import htmx from 'htmx.org';
import '../../main';
import { swapSettled } from '../testdata/htmx';

const DATA = '/src/components/testdata';

describe('inline dialog of the model manager (#841: age confirmation)', () => {
  let host: HTMLElement;

  const byId = (id: string) => document.getElementById(id)!;
  const dialog = () => host.querySelector<HTMLElement>('[role="dialog"]');

  function render(): void {
    host = document.createElement('div');
    host.innerHTML = `
      <div id="card">
        <button type="button" id="opener" hx-get="${DATA}/inline-dialog-open.html" hx-target="#mm-dialog" hx-swap="innerHTML">成人向けモデルを表示する…</button>
      </div>
      <div id="mm-dialog"></div>`;
    document.body.append(host);
    htmx.process(host);
  }

  async function open(): Promise<void> {
    byId('opener').focus();
    byId('opener').click();
    await waitUntil(() => dialog() !== null, 'the dialog did not arrive', { timeout: 2000 });
    await swapSettled();
  }

  beforeEach(render);
  afterEach(() => host.remove());

  it('moves the focus into the dialog when it opens', async () => {
    await open();
    expect(document.activeElement === dialog()).to.equal(true);
  });

  it('closes on Esc and gives the focus back to the button that opened it', async () => {
    await open();

    await sendKeys({ press: 'Escape' });
    await waitUntil(() => dialog() === null, 'Esc did not close the dialog', { timeout: 2000 });

    expect(document.activeElement === byId('opener')).to.equal(true);
  });

  it('closes on its own close button and gives the focus back to the opener', async () => {
    await open();

    (byId('dlg-close') as HTMLButtonElement).click();
    await waitUntil(() => dialog() === null, 'the close button did not close the dialog', { timeout: 2000 });

    expect(document.activeElement === byId('opener')).to.equal(true);
  });

  it('focuses the reason of a refusal, and Esc afterwards still returns to the opener', async () => {
    await open();

    (byId('dlg-submit') as HTMLButtonElement).click();
    await waitUntil(() => document.getElementById('dlg-error') !== null, 'the refusal was not drawn', {
      timeout: 2000,
    });
    await swapSettled();
    expect(document.activeElement === byId('dlg-error')).to.equal(true);

    await sendKeys({ press: 'Escape' });
    await waitUntil(() => dialog() === null, 'Esc did not close the dialog', { timeout: 2000 });
    expect(document.activeElement === byId('opener')).to.equal(true);
  });

  it('goes to the control of the place named by data-return-focus when the swap replaced the opener', async () => {
    await open();
    const oldOpener = byId('opener');
    // The answer of a confirmation: the card is drawn again and the dialog is emptied out of band.
    host.querySelector('#dlg-submit')!.closest('form')!.setAttribute('hx-get', `${DATA}/inline-dialog-saved.html`);
    host.querySelector('#dlg-submit')!.closest('form')!.setAttribute('hx-target', '#card');
    host.querySelector('#dlg-submit')!.closest('form')!.setAttribute('hx-swap', 'outerHTML');
    htmx.process(dialog()!);

    (byId('dlg-submit') as HTMLButtonElement).click();
    await waitUntil(() => dialog() === null, 'the dialog was not emptied', { timeout: 2000 });

    expect(oldOpener.isConnected).to.equal(false);
    expect(document.activeElement === byId('card-off')).to.equal(true);
  });

  it('leaves Esc alone when the focus is somewhere else on the page', async () => {
    await open();
    const other = document.createElement('button');
    host.append(other);
    other.focus();

    await sendKeys({ press: 'Escape' });

    expect(dialog() !== null).to.equal(true);
  });
});

describe('inline dialog host: the fade-out of a question that was closed (M5)', () => {
  let host: HTMLDialogElement;
  let style: HTMLStyleElement;

  beforeEach(() => {
    style = document.createElement('style');
    style.textContent =
      'dialog[data-inline-dialog-host]{opacity:0;transition:opacity 600ms linear,display 600ms allow-discrete,overlay 600ms allow-discrete}dialog[data-inline-dialog-host][open]{opacity:1}';
    document.head.append(style);
    host = document.createElement('dialog');
    host.setAttribute('data-inline-dialog-host', '');
    host.innerHTML = '<section role="dialog" data-inline-dialog aria-label="確認"><p>年齢確認</p></section>';
    document.body.append(host);
  });
  afterEach(() => {
    host.remove();
    style.remove();
  });

  it('keeps the question while it fades out, does not open again with a swap, and empties the host when the fade is over', async () => {
    host.showModal();
    expect(getComputedStyle(host).opacity).to.equal('1'); // the style the fade starts from
    const closed = new Promise((resolve) => host.addEventListener('close', resolve, { once: true }));
    host.close();
    await closed;
    expect(host.children.length).to.equal(1);

    document.body.dispatchEvent(new CustomEvent('htmx:afterSwap', { bubbles: true, detail: {} }));
    expect(host.open).to.equal(false);

    await waitUntil(() => host.children.length === 0, 'the host was not emptied after the fade', { timeout: 2000 });
    expect(host.open).to.equal(false);
  });
});
