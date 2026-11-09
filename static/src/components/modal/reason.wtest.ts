import { expect, waitUntil } from '@open-wc/testing';
import { sendKeys, setViewport } from '@web/test-runner-commands';
import htmx from 'htmx.org';
import '../../main';

const DATA = '/src/components/testdata/';

// #1118: the card of a model that cannot be installed has the short badge and the button 「理由と対処」, which opens the reason and what to do in the one modal of the page
// (the same dialog as the detail of a card). Opening it explains and installs nothing; Esc closes it and the focus goes back to the button; on a narrow, short screen the
// popup scrolls inside and its head (the title and the close button) is in view.
describe('the popup of the reason a model cannot be installed (#1118)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;
  let requests: string[];
  const record = (event: Event) => {
    const config = (event as CustomEvent<{ requestConfig?: { verb?: string; path?: string } }>).detail.requestConfig;
    requests.push(`${config?.verb?.toUpperCase()} ${config?.path}`);
  };

  const dialog = () => host.querySelector<HTMLDialogElement>('dialog[data-modal]')!;
  const button = () => host.querySelector<HTMLButtonElement>('[data-testid=mm-reason-open]')!;
  const card = () => host.querySelector<HTMLElement>('#mm-model-model-node')!;

  beforeEach(async function () {
    this.timeout(60_000); // the first request waits for the build of the CSS
    stylesheet = document.createElement('link');
    stylesheet.rel = 'stylesheet';
    stylesheet.href = '/__tailwind.css';
    const loaded = new Promise<void>((resolve, reject) => {
      stylesheet.onload = () => resolve();
      stylesheet.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
    });
    document.head.append(stylesheet);
    await loaded;
    const page = await (await fetch(`${DATA}layout-mm-card-node-needed.html`)).text();
    const modalHost = await (await fetch(`${DATA}mm-reason-host.html`)).text();
    host = document.createElement('div');
    host.innerHTML = `<main class="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 md:p-8">${page}</main>${modalHost}`;
    document.body.append(host);
    htmx.process(host);
    requests = [];
    document.addEventListener('htmx:beforeRequest', record);
  });

  afterEach(async () => {
    document.removeEventListener('htmx:beforeRequest', record);
    if (dialog()?.open) dialog().close();
    host?.remove();
    stylesheet.remove();
    await setViewport({ width: 800, height: 600 });
  });

  async function openReason(): Promise<void> {
    button().focus({ preventScroll: true });
    button().click();
    await waitUntil(() => dialog().open, 'the popup did not open', { timeout: 3000 });
    await waitUntil(() => !host.querySelector<HTMLElement>('#mam-modal-body')!.inert, 'the swap did not settle', {
      timeout: 3000,
    });
  }

  it('keeps the card short: the badge and the button, not the long explanation', () => {
    expect(card().querySelector('[data-testid=mm-nodes-badge]') !== null, 'the short badge stays').to.equal(true);
    expect(card().innerText).to.not.contain('導入できない');
    expect(card().innerText).to.not.contain('Python');
    expect(card().querySelector('[data-testid=mm-nodes-note]') === null, 'no long note on the card').to.equal(true);
    // the install button is there and disabled; the reason button is another control
    const install = [...card().querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent === '導入')!;
    expect(install.disabled).to.equal(true);
    expect(button() === install, 'the reason button is not the install button').to.equal(false);
  });

  it('opens the reason with a link to the nodes and installs nothing', async () => {
    await openReason();

    expect(document.getElementById('mam-modal-title')!.textContent).to.contain('導入できない理由と対処');
    expect(dialog().querySelector('[data-testid=mm-reason-items]')!.textContent).to.contain('Python');
    const link = dialog().querySelector<HTMLAnchorElement>('[data-testid=mm-reason-link]')!;
    expect(link.getAttribute('href')).to.equal('/model-manager/custom-nodes#mm-nodes');
    expect(link.hasAttribute('data-modal-close')).to.equal(true);
    expect(dialog().querySelector('[hx-post]') === null, 'nothing in the popup installs').to.equal(true);
    // the focus is inside the dialog, and it cannot leave it
    expect(dialog().contains(document.activeElement)).to.equal(true);
    button().focus();
    expect(document.activeElement === button()).to.equal(false);
    // the only request was the one for the popup
    expect(requests).to.deep.equal(['GET /model-manager/models/model-node/blocked']);
  });

  it('closes on Esc, and the focus goes back to the button that opened it', async () => {
    await openReason();

    await sendKeys({ press: 'Escape' });
    await waitUntil(() => !dialog().open, 'Esc did not close the popup', { timeout: 2000 });

    expect(document.activeElement === button()).to.equal(true);
    expect(requests.filter((r) => !r.startsWith('GET'))).to.deep.equal([]);
  });

  it('closes with its own button', async () => {
    await openReason();
    const close = [...dialog().querySelectorAll<HTMLButtonElement>('button[data-modal-close]')].find(
      (b) => b.textContent?.trim() === '閉じる',
    )!;

    close.click();

    expect(dialog().open).to.equal(false);
    expect(document.activeElement === button()).to.equal(true);
  });

  it('on a narrow, short screen scrolls inside and shows its head: the title and the close button', async () => {
    await setViewport({ width: 390, height: 420 });
    await openReason();
    const dlg = dialog();
    const title = document.getElementById('mam-modal-title')!;
    const close = dlg.querySelector<HTMLElement>('button[aria-label="閉じる"]')!;
    const inView = (el: Element) => {
      const r = el.getBoundingClientRect();
      return r.top >= 0 && r.bottom <= window.innerHeight && r.left >= 0 && r.right <= window.innerWidth;
    };

    expect(dlg.scrollHeight, 'the popup is taller than the screen, so it scrolls').to.be.greaterThan(dlg.clientHeight);
    expect(dlg.getBoundingClientRect().width).to.be.at.most(390);
    expect(dlg.scrollTop).to.equal(0);
    expect(inView(title), 'the title is cut off').to.equal(true);
    expect(inView(close), 'the close button is cut off').to.equal(true);
    expect(document.documentElement.scrollWidth).to.be.at.most(390);

    // scrolled to the foot, the head is still in reach (it sticks to the top of the dialog)
    dlg.scrollTop = dlg.scrollHeight;
    expect(inView(title)).to.equal(true);
    expect(inView(close)).to.equal(true);
  });
});
