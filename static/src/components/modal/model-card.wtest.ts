import { expect, waitUntil } from '@open-wc/testing';
import { sendKeys } from '@web/test-runner-commands';
import htmx from 'htmx.org';
import '../../main';

const DATA = '/src/components/testdata';

// The card of the model manager (#954) opens the one modal of the page and is replaced whole by the poll of an install that is running
// (hx-trigger="every ..."): the dialog stands outside what is swapped, so the swap leaves it, what it shows, the focus inside it and
// the scroll position alone, and the focus goes back to the new card's own button when it closes.
describe('model card and its detail modal (#954)', () => {
  let host: HTMLElement;

  const dialog = () => host.querySelector<HTMLDialogElement>('dialog')!;
  const card = () => document.getElementById('mm-model-a')!;
  const opener = () => card().querySelector<HTMLButtonElement>('[data-card-open]')!;

  beforeEach(async () => {
    host = document.createElement('div');
    host.innerHTML = `
      <div style="height: 3000px">spacer</div>
      <ul id="mm-models"><li id="mm-model-a" class="card" data-card>
        <h3><button type="button" data-card-open data-modal-open data-focus-key="open-mm-model-a" aria-haspopup="dialog"
          hx-get="${DATA}/model-card-detail.html" hx-target="#mam-modal-body" hx-swap="innerHTML">モデル A</button></h3>
      </li></ul>
      <dialog id="mam-modal" data-modal aria-modal="true" aria-labelledby="mam-modal-title">
        <div id="mam-modal-body"></div>
        <p id="mam-modal-failure" role="alert" hidden></p>
        <button type="button" data-modal-close aria-label="閉じる">×</button>
      </dialog>`;
    document.body.append(host);
    htmx.process(host);
    window.scrollTo(0, 1200);
    await waitUntil(() => Math.abs(window.scrollY - 1200) < 2, 'the page did not scroll');
  });

  afterEach(() => {
    if (dialog().open) dialog().close();
    host.remove();
    window.scrollTo(0, 0);
  });

  async function openDetail(): Promise<void> {
    opener().focus({ preventScroll: true });
    opener().click();
    await waitUntil(() => dialog().open, 'the dialog did not open', { timeout: 2000 });
    await waitUntil(() => !host.querySelector<HTMLElement>('#mam-modal-body')!.inert, 'the swap did not settle', {
      timeout: 2000,
    });
  }

  it('is a modal dialog named by the heading of the detail, with the files and their SHA-256', async () => {
    await openDetail();

    expect(dialog().getAttribute('aria-modal')).to.equal('true');
    expect(dialog().getAttribute('aria-labelledby')).to.equal('mam-modal-title');
    expect(document.getElementById('mam-modal-title')!.textContent).to.equal('モデル A');
    expect(host.querySelector('[data-testid="mm-sha256"]')!.textContent).to.equal('0123abcd');
    // the focus starts on the top of the detail (a long read does not start at its foot), inside the dialog, and cannot leave it
    expect((document.activeElement as HTMLElement).dataset.testid).to.equal('mm-model-detail');
    expect(dialog().contains(document.activeElement)).to.equal(true);
    opener().focus();
    expect(document.activeElement === opener()).to.equal(false);
  });

  it('closes on Esc and gives the focus back to the card that opened it, without moving the page', async () => {
    const before = window.scrollY;
    await openDetail();

    await sendKeys({ press: 'Escape' });
    await waitUntil(() => !dialog().open, 'Esc did not close the dialog', { timeout: 2000 });

    expect(document.activeElement === opener()).to.equal(true);
    expect(host.querySelector('#mam-modal-body')!.children.length).to.equal(0);
    expect(Math.abs(window.scrollY - before)).to.be.lessThan(2);
  });

  it('keeps the open modal, its focus and the scroll position when the poll replaces the card, then returns the focus to the new card', async () => {
    await openDetail();
    const heading = document.getElementById('mam-modal-title')!;
    const first = card();
    // the card of an install that is running polls itself: htmx replaces the whole <li>
    first.setAttribute('hx-get', `${DATA}/model-card-poll.html`);
    first.setAttribute('hx-trigger', 'every 200ms');
    first.setAttribute('hx-swap', 'outerHTML');
    htmx.process(first);
    await waitUntil(() => card() !== first && card().dataset.polled === 'yes', 'the poll did not replace the card', {
      timeout: 4000,
    });
    const second = card();
    await waitUntil(() => card() !== second, 'the poll did not replace the card again', { timeout: 4000 });

    expect(dialog().open).to.equal(true);
    expect(document.getElementById('mam-modal-title')).to.equal(heading);
    expect(dialog().contains(document.activeElement)).to.equal(true);
    expect(Math.abs(window.scrollY - 1200)).to.be.lessThan(2);

    await sendKeys({ press: 'Escape' });
    await waitUntil(() => !dialog().open, 'Esc did not close the dialog', { timeout: 2000 });
    expect(first.isConnected).to.equal(false);
    expect(document.activeElement === opener()).to.equal(true);
    expect(Math.abs(window.scrollY - 1200)).to.be.lessThan(2);
  });
});
