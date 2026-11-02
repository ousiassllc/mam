import { expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// #925: the history of the generation test refreshes itself while a result runs (the part `#gt-history-live`, every 2 s), has a
// refresh button, and never takes what the reader is doing: the filter lies outside of the part that is swapped, and the focus
// goes back to the button that was replaced. The pages are what the handler renders (backend/internal/web/gentest_live_fixture_test.go).
const TESTDATA = '/src/components/testdata/';

describe('the history of the generation test refreshes itself (#925)', () => {
  let host: HTMLElement;

  const byTestId = (id: string) => host.querySelector<HTMLElement>(`[data-testid="${id}"]`)!;
  const statuses = () =>
    [...host.querySelectorAll('[data-testid="gt-card"]')].map((c) => c.getAttribute('data-status'));

  async function show(fixture: string): Promise<void> {
    host = document.createElement('div');
    host.innerHTML = await (await fetch(`${TESTDATA}${fixture}`)).text();
    document.body.append(host);
    htmx.process(host);
  }

  afterEach(() => host.remove());

  it('swaps in the finished results by itself and stops asking when nothing runs any more', async function () {
    this.timeout(20_000);
    await show('gentest-history-running.html');
    expect(statuses()).to.deep.equal(['succeeded', 'succeeded', 'running']);
    expect(byTestId('gt-history-live').getAttribute('hx-trigger')).to.equal('every 2s');

    await waitUntil(() => !statuses().includes('running'), 'the finished result did not arrive', { timeout: 8000 });

    expect(statuses()).to.deep.equal(['succeeded', 'succeeded', 'succeeded']);
    expect(byTestId('gt-updated').textContent).to.contain('10:00:05');
    expect(byTestId('gt-history-live').hasAttribute('hx-trigger'), 'it still asks again').to.equal(false);
  });

  it('does not take the choice the reader is making in the filter', async function () {
    this.timeout(20_000);
    await show('gentest-history-running.html');
    const select = host.querySelector<HTMLSelectElement>('#gt-filter-model')!;
    select.value = 'nano-banana';
    select.focus();

    await waitUntil(() => !statuses().includes('running'), 'the finished result did not arrive', { timeout: 8000 });

    expect(host.querySelector<HTMLSelectElement>('#gt-filter-model'), 'the filter was drawn again').to.equal(select);
    expect(select.value).to.equal('nano-banana');
    expect(document.activeElement).to.equal(select);
  });

  it('reads the history again when the refresh button is pressed, and the focus stays on the button', async function () {
    this.timeout(20_000);
    await show('gentest-history-idle.html');
    expect(byTestId('gt-history-live').hasAttribute('hx-trigger'), 'nothing runs, so nothing is asked for').to.equal(
      false,
    );
    expect(byTestId('gt-updated').textContent).to.contain('10:00:00');
    const button = byTestId('gt-refresh') as HTMLButtonElement;
    expect(button.getAttribute('aria-label')).to.equal('更新');
    button.focus();

    button.click();
    await waitUntil(() => byTestId('gt-updated').textContent?.includes('10:00:05'), 'the history was not read again', {
      timeout: 4000,
    });

    expect(byTestId('gt-refresh'), 'the button was not drawn again').not.to.equal(button);
    expect(document.activeElement, 'the focus is not on the button').to.equal(byTestId('gt-refresh'));
  });

  describe('the poll waits while it would spoil what the reader is doing', () => {
    /** The event htmx sends before a request: cancelled, the request does not go out. */
    function beforeRequest(elt: Element, triggeringEvent?: Event): boolean {
      const event = new CustomEvent('htmx:beforeRequest', {
        bubbles: true,
        cancelable: true,
        detail: { elt, requestConfig: { triggeringEvent } },
      });
      elt.dispatchEvent(event);
      return !event.defaultPrevented;
    }

    const poller = () => byTestId('gt-history-live');

    it('lets a poll go out when nothing is playing', async () => {
      await show('gentest-history-running.html');
      expect(beforeRequest(poller())).to.equal(true);
    });

    it('holds a poll back while a video of the section plays, and lets the refresh button and a click through', async () => {
      await show('gentest-history-running.html');
      const video = document.createElement('video');
      Object.defineProperty(video, 'paused', { value: false });
      poller().append(video);

      expect(beforeRequest(poller()), 'the poll went out over a playing video').to.equal(false);
      expect(
        beforeRequest(byTestId('gt-refresh'), new MouseEvent('click')),
        'the button of the reader was held back',
      ).to.equal(true);
      expect(beforeRequest(poller(), new MouseEvent('click')), 'a request of the reader was held back').to.equal(true);
    });

    it('holds a poll back while the tab is hidden', async () => {
      await show('gentest-history-running.html');
      Object.defineProperty(document, 'hidden', { configurable: true, get: () => true });
      try {
        expect(beforeRequest(poller())).to.equal(false);
      } finally {
        Reflect.deleteProperty(document, 'hidden'); // back to the prototype's getter
      }
      expect(beforeRequest(poller())).to.equal(true);
    });
  });
});
