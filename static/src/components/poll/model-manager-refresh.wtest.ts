import { expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// #962: the engine card of the model manager asks for itself every 2 s while an install runs (the poll guard holds it back while the tab is
// hidden), has the refresh button of the history (#925), and neither takes the modal that is open. The files are what the handler
// renders (backend/internal/web/mm_refresh_fixture_test.go); the answer of the dev server is in web-test-runner.config.mjs.
const TESTDATA = '/src/components/testdata/';

describe('the engine card of the model manager refreshes itself (#962)', () => {
  let host: HTMLElement;

  const card = () => host.querySelector<HTMLElement>('#mm-engine')!;
  const byTestId = (id: string) => host.querySelector<HTMLElement>(`[data-testid="${id}"]`)!;
  const dialog = () => host.querySelector<HTMLDialogElement>('dialog')!;

  async function show(fixture: string): Promise<void> {
    host = document.createElement('div');
    host.innerHTML = `<ul>${await (await fetch(`${TESTDATA}${fixture}`)).text()}</ul>
      <div id="mm-catalog"></div>
      <dialog id="mam-modal" data-modal aria-modal="true" aria-labelledby="mam-modal-title">
        <div id="mam-modal-body"><h2 id="mam-modal-title">ComfyUI エンジン</h2><p>開いている詳細</p></div>
        <button type="button" data-modal-close aria-label="閉じる">×</button>
      </dialog>`;
    document.body.append(host);
    htmx.process(host);
  }

  afterEach(() => {
    if (dialog()?.open) dialog().close();
    host.remove();
  });

  it('swaps in the finished install by itself and stops asking', async function () {
    this.timeout(20_000);
    await show('mm-engine-installing.html');
    expect(card().getAttribute('data-engine')).to.equal('installing');
    expect(card().getAttribute('hx-trigger')).to.equal('every 2s');

    await waitUntil(() => card().getAttribute('data-engine') === 'installed', 'the finished install did not arrive', {
      timeout: 8000,
    });

    expect(card().hasAttribute('hx-trigger'), 'it still asks again').to.equal(false);
    expect(byTestId('refresh-updated').textContent).to.contain('10:00:05');
  });

  it('leaves the modal that is open alone while the poll swaps the card', async function () {
    this.timeout(20_000);
    await show('mm-engine-installing.html');
    dialog().showModal();

    await waitUntil(() => card().getAttribute('data-engine') === 'installed', 'the finished install did not arrive', {
      timeout: 8000,
    });

    expect(dialog().open, 'the modal was closed by the swap').to.equal(true);
    expect(dialog().textContent).to.contain('開いている詳細');
  });

  it('reads the card again when the refresh button is pressed, and the focus stays on the button', async function () {
    this.timeout(20_000);
    await show('mm-engine-idle.html');
    expect(byTestId('refresh-updated').textContent).to.contain('10:00:00');
    const button = byTestId('refresh') as HTMLButtonElement;
    expect(button.getAttribute('aria-label')).to.equal('更新');
    button.focus();

    button.click();
    await waitUntil(
      () => byTestId('refresh-updated').textContent?.includes('10:00:05'),
      'the card was not read again',
      {
        timeout: 4000,
      },
    );

    expect(byTestId('refresh'), 'the button was not drawn again').not.to.equal(button);
    expect(document.activeElement, 'the focus is not on the button').to.equal(byTestId('refresh'));
  });

  it('holds the poll back while the tab is hidden, and lets the refresh button through', async () => {
    await show('mm-engine-installing.html');
    const beforeRequest = (elt: Element, triggeringEvent?: Event) => {
      const event = new CustomEvent('htmx:beforeRequest', {
        bubbles: true,
        cancelable: true,
        detail: { elt, requestConfig: { triggeringEvent } },
      });
      elt.dispatchEvent(event);
      return !event.defaultPrevented;
    };
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true });
    try {
      expect(beforeRequest(card()), 'the poll went out of a hidden tab').to.equal(false);
    } finally {
      Reflect.deleteProperty(document, 'hidden');
    }
    expect(beforeRequest(card())).to.equal(true);
  });
});
