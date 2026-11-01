import { expect, waitUntil } from '@open-wc/testing';
import { sendMouse, setViewport } from '@web/test-runner-commands';
import htmx from 'htmx.org';
import '../../main';

// #908: pressing 導入 in /model-manager threw the page to the bottom. The page is the real screen, drawn with the real
// Tailwind CSS, and the answers are what the handlers render (backend/internal/web/scroll_fixture_test.go keeps them
// current), so what moves the page here is what moves it for a reader.
//
// What was found in a real Chrome (headless, against the real handlers): the plain install and the polling swaps leave the
// page where it is (Chrome's scroll anchoring holds the reader's place while the list above and below is redrawn), and
// the first two tests keep it so. What moved the page was the license question: its dialog stands above the list, the
// focus that went into it (and back to the card afterwards) had no `preventScroll`, so closing it threw the page from the
// dialog at the top of the list to the card at the bottom of it. The last two tests fail without the fix.
const TESTDATA = '/src/components/testdata/';

describe('the model manager keeps the page where the reader is (#908)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  const byTestId = (root: ParentNode, id: string) => root.querySelector<HTMLElement>(`[data-testid="${id}"]`)!;
  const card = (id: string) => host.querySelector<HTMLElement>(`[data-model="${id}"]`);
  const installButton = () => card('model-5')!.querySelector<HTMLButtonElement>('[data-testid="mm-install"]')!;

  /** Every position the page took from the click on, so that a jump that came back is not missed. */
  let seen: number[];
  const onScroll = () => seen.push(window.scrollY);

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
    await setViewport({ width: 1280, height: 800 });

    const html = await (await fetch(`${TESTDATA}scroll-mm-page.html`)).text();
    host = document.createElement('div');
    host.className = 'mx-auto max-w-5xl p-6';
    host.innerHTML = html;
    document.body.append(host);
    htmx.process(host);
    seen = [];
    window.addEventListener('scroll', onScroll);
  });

  afterEach(() => {
    window.removeEventListener('scroll', onScroll);
    host.remove();
    stylesheet.remove();
    window.scrollTo(0, 0);
  });

  /** Puts the install button of the 5th model in the middle of the window, as a reader who scrolled down to it. */
  async function scrollToInstall(): Promise<number> {
    installButton().scrollIntoView({ block: 'center' });
    await waitUntil(() => window.scrollY > 0, 'the page did not scroll to the button');
    expect(
      document.documentElement.scrollHeight - window.innerHeight - window.scrollY,
      'the test no longer shows a page with something below the list',
    ).to.be.greaterThan(500);
    return window.scrollY;
  }

  /** The next time htmx has settled a swap (the answer is drawn, the focus given back). */
  const settled = () =>
    new Promise<void>((resolve) => document.body.addEventListener('htmx:afterSettle', () => resolve(), { once: true }));

  it('does not move the page when 導入 is pressed', async () => {
    const before = await scrollToInstall();
    seen = [];

    const done = settled();
    const box = installButton().getBoundingClientRect();
    await sendMouse({
      type: 'click',
      position: [Math.round(box.x + box.width / 2), Math.round(box.y + box.height / 2)],
    });
    await done;
    await waitUntil(() => card('model-5')?.getAttribute('data-state') === 'installing', 'the list was not drawn');
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));

    expect(byTestId(host, 'mm-catalog-notice').textContent).to.contain('導入を始めました');
    expect(window.scrollY, `the page moved from ${before} (positions seen: ${seen.join(', ')})`).to.equal(before);
    expect(
      seen.filter((y) => y !== before),
      'the page moved on the way',
    ).to.deep.equal([]);
  });

  it('does not move the page when the polling ends and the list and the trial section are drawn again', async function () {
    this.timeout(20_000);
    const before = await scrollToInstall();
    installButton().click();
    await waitUntil(() => card('model-5')?.getAttribute('data-state') === 'installing', 'the install did not start');
    seen = [];

    // The list asks for itself every 2 s; the answer is the finished list with the trial section out of band.
    await waitUntil(() => card('model-5')?.getAttribute('data-state') === 'installed', 'the polling did not end', {
      timeout: 8000,
    });
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));

    expect(window.scrollY, `the page moved from ${before} (positions seen: ${seen.join(', ')})`).to.equal(before);
    expect(
      seen.filter((y) => y !== before),
      'the page moved on the way',
    ).to.deep.equal([]);
  });

  /** Presses a control the way a reader does: the mouse, at the middle of the control. */
  async function press(el: HTMLElement): Promise<void> {
    const box = el.getBoundingClientRect();
    await sendMouse({
      type: 'click',
      position: [Math.round(box.x + box.width / 2), Math.round(box.y + box.height / 2)],
    });
  }

  const frames = () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const dialog = () => host.querySelector<HTMLElement>('[data-inline-dialog]');
  const inView = (el: HTMLElement) => {
    const box = el.getBoundingClientRect();
    return box.top >= 0 && box.bottom <= window.innerHeight;
  };

  describe('the license question of a model at the end of a long list', () => {
    let opener: HTMLButtonElement;
    let before: number;

    beforeEach(async () => {
      opener = card('model-8')!.querySelector<HTMLButtonElement>('[data-testid="mm-consent-open"]')!;
      opener.scrollIntoView({ block: 'center' });
      await waitUntil(() => window.scrollY > 0, 'the page did not scroll to the button');
      before = window.scrollY;
      await press(opener);
      await waitUntil(() => dialog() !== null, 'the dialog did not arrive');
      await frames();
    });

    it('brings the question into view when it opens, and puts the page back when it is closed', async () => {
      expect(inView(dialog()!), 'the question is out of sight').to.equal(true);
      expect(window.scrollY).to.be.lessThan(before);

      await press(host.querySelector<HTMLButtonElement>('[data-inline-dialog-close]')!);
      await waitUntil(() => dialog() === null, 'the dialog did not close');
      await frames();

      expect(window.scrollY, 'the page is not where it was when the button was pressed').to.equal(before);
      expect(document.activeElement === opener || opener.isConnected === false).to.equal(true);
    });

    it('brings a different question into view when it replaces the one that is open and the reader is far from it', async () => {
      const first = dialog()!;
      // The reader went back down to the list, away from the open question, and presses another control that asks.
      opener.scrollIntoView({ block: 'center' });
      await waitUntil(() => !inView(first), 'the first question is still in sight');
      await press(opener);
      await waitUntil(() => dialog() !== null && dialog() !== first, 'the second question did not arrive');
      await frames();

      expect(inView(dialog()!), 'the question that replaced the first one is out of sight').to.equal(true);
    });

    it('puts the page back where it was when the license is accepted and the install starts', async () => {
      const agree = host.querySelector<HTMLInputElement>('[data-testid="mm-agree"]')!;
      await press(agree);
      const done = settled();
      await press(host.querySelector<HTMLButtonElement>('[data-testid="mm-consent-form"] button[type="submit"]')!);
      await done;
      await waitUntil(() => card('model-8')?.getAttribute('data-state') === 'installing', 'the list was not drawn');
      await frames();

      expect(dialog()).to.equal(null);
      expect(window.scrollY, 'the page moved to the card, or stayed at the emptied dialog').to.equal(before);
    });
  });
});
