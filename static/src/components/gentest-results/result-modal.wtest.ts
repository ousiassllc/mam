import { expect, waitUntil } from '@open-wc/testing';
import { setViewport } from '@web/test-runner-commands';
import htmx from 'htmx.org';
import { stubConfirm } from '../confirm/testkit';
import '../../main';

// #1004: the results of the history are a grid of pictures; a tap on one opens its detail in the modal (the model, the state, the time, the prompt, the
// settings, the large picture and what can be done with it), and what is done there shows on the grid behind it: the heart of a favorite, the tile that was
// deleted. The pages are what the handlers render (backend/internal/web/gentest_live_fixture_test.go); the answers of the requests are served by
// web-test-runner.config.mjs (gentestResultAnswers).
const TESTDATA = '/src/components/testdata/';

describe('a tile of the history opens the detail of the result in the modal (#1004)', () => {
  let host: HTMLElement;
  let confirm: ReturnType<typeof stubConfirm>;

  const byTestId = (id: string) => host.querySelector<HTMLElement>(`[data-testid="${id}"]`)!;
  const tile = (id: string) => host.querySelector<HTMLElement>(`[data-result="${id}"]`)!;
  const dialog = () => host.querySelector<HTMLDialogElement>('dialog[data-modal]')!;
  const inModal = (id: string) => dialog().querySelector<HTMLElement>(`[data-testid="${id}"]`);

  async function show(fixture = 'gentest-history-idle.html'): Promise<void> {
    host = document.createElement('div');
    host.innerHTML =
      (await (await fetch(`${TESTDATA}${fixture}`)).text()) +
      (await (await fetch(`${TESTDATA}gentest-modal-host.html`)).text());
    document.body.append(host);
    htmx.process(host);
    htmx.trigger(host, 'htmx:load');
  }

  async function open(id: string): Promise<void> {
    tile(id).querySelector<HTMLElement>('[data-testid="gt-open"]')!.click();
    await waitUntil(() => dialog().open, 'the modal did not open', { timeout: 3000 });
    await waitUntil(() => !dialog().querySelector<HTMLElement>('#mam-modal-body')!.inert, 'the swap did not settle', {
      timeout: 3000,
    });
  }

  beforeEach(() => {
    confirm = stubConfirm(true);
  });
  afterEach(() => {
    confirm.restore();
    if (dialog()?.open) dialog().close();
    host.remove();
  });

  it('is a grid of pictures only: no prompt, no date and no buttons on a tile', async () => {
    await show();
    const first = host.querySelector<HTMLElement>('[data-testid="gt-history-item"]')!;
    expect(first.querySelector('img'), 'the picture').not.to.equal(null);
    expect(first.querySelector('p'), 'a line of text on the tile').to.equal(null);
    expect(first.querySelector('button'), 'a button on the tile').to.equal(null);
    expect(first.textContent!.trim(), 'only the hidden names are text').to.not.contain('a fox');
    expect(tile('r0').querySelector('a')!.getAttribute('aria-label'), 'the name of the link').to.equal(
      'モデル・完了・詳細を開く',
    );
  });

  it('shows the state of a result that has no file as a badge on its tile', async () => {
    await show('gentest-history-running.html');
    const badge = tile('r2').querySelector('[data-testid="gt-card-status"]');
    expect(badge?.textContent).to.equal('生成中');
    expect(tile('r0').querySelector('[data-testid="gt-card-status"]'), 'a finished result has none').to.equal(null);
  });

  it('opens with the detail on a tap: the model, the state, the time, the prompt, the settings and the large picture', async () => {
    await show();
    await open('r0');

    expect(dialog().getAttribute('aria-labelledby')).to.equal('mam-modal-title');
    expect(dialog().querySelector('#mam-modal-title')!.textContent).to.equal('FLUX.2');
    expect(inModal('gt-card-status')!.textContent).to.equal('完了');
    expect(inModal('gt-detail-time')!.textContent).to.equal('2026-11-02 10:00');
    expect(inModal('gt-sent-prompt')!.textContent).to.equal('a fox 0');
    expect(inModal('gt-params')!.textContent).to.contain('縦横比');
    expect(dialog().querySelector('img')!.getAttribute('src')).to.contain('data:image/svg+xml');
    expect(inModal('gt-download'), 'the download').not.to.equal(null);
    expect(inModal('gt-delete'), 'the delete').not.to.equal(null);
    expect(inModal('gt-favorite'), 'the favorite').not.to.equal(null);
    expect(dialog().querySelector('[data-modal-wide]'), 'the wide dialog is asked for').not.to.equal(null);
  });

  it('puts the focus on the detail and gives it back to the tile when it closes', async () => {
    await show();
    const link = tile('r0').querySelector<HTMLElement>('[data-testid="gt-open"]')!;
    link.focus();
    await open('r0');
    expect(dialog().contains(document.activeElement), `the focus is on ${document.activeElement?.tagName}`).to.equal(
      true,
    );

    dialog().querySelector<HTMLElement>('[data-modal-close]')!.click();
    expect(dialog().open).to.equal(false);
    await waitUntil(() => document.activeElement === link, 'the focus did not go back to the tile'); // `close` is told a task later
  });

  it('shows a favorite set in the modal as the heart of the tile', async () => {
    await show();
    expect(tile('r0').querySelector('[data-testid="gt-fav-mark"]')!.hasAttribute('hidden')).to.equal(true);
    await open('r0');

    inModal('gt-favorite')!.click();
    await waitUntil(
      () => !tile('r0').querySelector('[data-testid="gt-fav-mark"]')!.hasAttribute('hidden'),
      'the heart did not show',
    );
    expect(tile('r0').querySelector('[data-testid="gt-fav-mark"]')!.textContent).to.contain('★');
  });

  it('takes the tile out of the grid when the result is deleted in the modal, and closes', async () => {
    await show();
    expect(tile('r0')).not.to.equal(null);
    await open('r0');

    inModal('gt-delete')!.click();
    await waitUntil(() => !dialog().open, 'the modal did not close');
    expect(host.querySelector('[data-result="r0"]'), 'the tile is still there').to.equal(null);
    expect(host.querySelectorAll('[data-testid="gt-history-item"]').length, 'the others stay').to.equal(2);
  });

  it('does not delete when the confirmation is refused', async () => {
    await show();
    await open('r0');
    confirm.restore();
    confirm = stubConfirm(false);

    const asked = new Promise((resolve) => host.addEventListener('htmx:confirm', resolve, { once: true }));
    inModal('gt-delete')!.click();
    await asked; // htmx asks, the answer is no: the request is never sent
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    expect(dialog().open).to.equal(true);
    expect(tile('r0')).not.to.equal(null);
  });

  describe('the choice for the ZIP export', () => {
    const modalBox = () => dialog().querySelector<HTMLInputElement>('[data-gt-modal-pick]')!;
    const count = () => byTestId('gt-selected-count').textContent;

    it('is offered in the modal, shows what the grid has chosen and chooses for the grid', async () => {
      await show();
      host.querySelector<HTMLElement>('[data-gt-pick="r0"] [data-testid="gt-pick-mark"]')!.click();
      expect(count()).to.equal('1');
      await open('r0');

      expect(modalBox().checked, 'the grid chose it').to.equal(true);
      modalBox().click();
      expect(count(), 'the modal let it go').to.equal('0');
      expect(host.querySelector<HTMLInputElement>('[data-gt-pick="r0"] input')!.checked).to.equal(false);

      modalBox().click();
      expect(count()).to.equal('1');
      expect(host.querySelector<HTMLInputElement>('[data-gt-pick="r0"] input')!.checked).to.equal(true);
    });
  });

  // #1097: the detail is taller than the dialog, which scrolls (the dialog is the scroller). It was opened scrolled down, with the model, the state and the
  // close button above the top edge: showModal() scrolls the autofocus block into view, and a closed dialog comes back at the place it was scrolled to.
  // These tests draw the real markup with the real Tailwind CSS in a window that is low (a phone held sideways), and the answer is made long.
  describe('the head of a long detail (#1097)', () => {
    let stylesheet: HTMLLinkElement;
    const padLong = (event: Event) => {
      const detail = (event as CustomEvent<{ serverResponse: string }>).detail;
      const rows = Array.from(
        { length: 30 },
        (_, i) => `<p class="rounded-md border p-2 text-sm">行 ${i + 1}</p>`,
      ).join('');
      detail.serverResponse = detail.serverResponse.replace('</section>', `${rows}</section>`);
    };
    const inView = (el: Element) => {
      const box = el.getBoundingClientRect();
      const frame = dialog().getBoundingClientRect();
      return box.top >= frame.top - 0.5 && box.bottom <= frame.bottom + 0.5;
    };
    const head = () => ({
      title: dialog().querySelector<HTMLElement>('#mam-modal-title')!,
      close: dialog().querySelector<HTMLElement>('[data-modal-close]')!,
    });
    const expectHeadInView = (when: string) => {
      expect(dialog().scrollTop, `${when}: where the dialog is scrolled to`).to.equal(0);
      expect(inView(head().title), `${when}: the title`).to.equal(true);
      expect(inView(head().close), `${when}: the close button`).to.equal(true);
    };

    beforeEach(async () => {
      await setViewport({ width: 1280, height: 400 });
      stylesheet = document.createElement('link');
      stylesheet.rel = 'stylesheet';
      stylesheet.href = '/__tailwind.css';
      await new Promise<void>((resolve, reject) => {
        stylesheet.onload = () => resolve();
        stylesheet.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
        document.head.append(stylesheet);
      });
      document.addEventListener('htmx:beforeSwap', padLong);
    });
    afterEach(async () => {
      document.removeEventListener('htmx:beforeSwap', padLong);
      stylesheet.remove();
      await setViewport({ width: 800, height: 600 });
    });

    it('has the title and the close button in view right after it opens', async () => {
      await show();
      await open('r0');
      expect(dialog().scrollHeight, 'the detail must be taller than the dialog for this test').to.be.greaterThan(
        dialog().clientHeight + 200,
      );
      expectHeadInView('opened');
    });

    it('starts from the top again when the same dialog opens another result after it was scrolled', async () => {
      await show();
      await open('r0');
      dialog().scrollTop = 500;
      expect(dialog().scrollTop, 'the body scrolls').to.be.greaterThan(0);
      // `close` is fired in a task of its own: wait for it, or it could empty the body of the next opening.
      const closed = new Promise((resolve) => dialog().addEventListener('close', resolve, { once: true }));
      dialog().close();
      await closed;

      await open('r1');
      expectHeadInView('opened again');
    });

    it('starts from the top when a control of the open dialog asks for another opening', async () => {
      await show();
      await open('r0');
      dialog().scrollTop = 500;

      const next = document.createElement('a');
      next.setAttribute('hx-get', '/generation-test/results/r2');
      next.setAttribute('hx-target', '#mam-modal-body');
      next.setAttribute('hx-swap', 'innerHTML');
      next.setAttribute('data-modal-open', '');
      dialog().querySelector('#mam-modal-body')!.append(next);
      htmx.process(next);
      const swapped = new Promise((resolve) => host.addEventListener('htmx:afterSettle', resolve, { once: true }));
      next.click();
      await swapped;
      expectHeadInView('swapped in place');
    });

    it('keeps the title and the close button at the top while the body scrolls', async () => {
      await show();
      await open('r0');

      dialog().scrollTop = 500;
      expect(dialog().scrollTop, 'the long body scrolls').to.equal(500);
      expect(inView(head().title), 'the title').to.equal(true);
      expect(inView(head().close), 'the close button').to.equal(true);

      dialog().scrollTop = dialog().scrollHeight;
      expect(dialog().scrollTop, 'the end of the body is reachable').to.be.greaterThan(500);
      expect(inView(head().title), 'the title at the end').to.equal(true);
      expect(inView(head().close), 'the close button at the end').to.equal(true);
    });
  });
});
