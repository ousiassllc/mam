import { expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// #932: a tile of the history is chosen for the ZIP export by the check box in its corner (the peer of its glowing frame; a tap on the tile itself opens the
// detail in the modal, #1004), and the choice survives the refresh of the history, which draws the tiles again every 2 s while a result runs (#925). The pages are what the handler renders
// (backend/internal/web/gentest_live_fixture_test.go): three results r0 r1 r2, of which r2 runs in the "running" page and has
// finished in the answer of the poll.
const TESTDATA = '/src/components/testdata/';

describe('the results of the history are chosen for the ZIP export by a tap (#932)', () => {
  let host: HTMLElement;

  const byTestId = (id: string) => host.querySelector<HTMLElement>(`[data-testid="${id}"]`)!;
  const card = (id: string) => host.querySelector<HTMLElement>(`[data-gt-pick="${id}"]`)!;
  const input = (id: string) => card(id).querySelector<HTMLInputElement>('[data-gt-pick-input]')!;
  /** The tap of the mouse on the mark in the corner of a tile (the label of its checkbox). */
  const tapMark = (id: string) => card(id).querySelector<HTMLElement>('[data-testid="gt-pick-mark"]')!.click();
  const count = () => byTestId('gt-selected-count').textContent;
  const hiddenIds = () =>
    [...host.querySelectorAll<HTMLInputElement>('[data-gt-export-selected] input[name="id"]')].map((i) => i.value);
  const statuses = () =>
    [...host.querySelectorAll('[data-testid="gt-card"]')].map((c) => c.getAttribute('data-status'));

  async function show(fixture: string): Promise<void> {
    host = document.createElement('div');
    host.innerHTML = await (await fetch(`${TESTDATA}${fixture}`)).text();
    // A tap on the tile (a link to the detail) must not leave the test page.
    host.addEventListener('click', (event) => {
      if ((event.target as Element).closest('a')) event.preventDefault();
    });
    document.body.append(host);
    htmx.process(host);
    htmx.trigger(host, 'htmx:load');
  }

  afterEach(() => host.remove());

  it('glows and counts a tapped card, and lets it go with another tap', async () => {
    await show('gentest-history-idle.html');
    expect(count()).to.equal('0');
    expect((byTestId('gt-export-selected-submit') as HTMLButtonElement).disabled, 'nothing is chosen').to.equal(true);

    tapMark('r1');
    expect(input('r1').checked, 'the tapped card is chosen').to.equal(true);
    expect(input('r0').checked).to.equal(false);
    expect(count()).to.equal('1');
    expect(hiddenIds()).to.deep.equal(['r1']);
    expect((byTestId('gt-export-selected-submit') as HTMLButtonElement).disabled).to.equal(false);

    tapMark('r0');
    expect(hiddenIds().sort()).to.deep.equal(['r0', 'r1']);
    expect(count()).to.equal('2');

    tapMark('r1');
    expect(input('r1').checked).to.equal(false);
    expect(hiddenIds()).to.deep.equal(['r0']);
  });

  it('is chosen from the keyboard with the checkbox of the card, and the choice is cleared by the clear button', async () => {
    await show('gentest-history-idle.html');
    const box = input('r2');
    expect(box.getAttribute('aria-label')).to.equal('書き出しに選ぶ');
    expect(box.checked).to.equal(false);
    box.focus();
    box.click(); // Space on a focused checkbox is a click
    expect(box.checked).to.equal(true);
    expect(hiddenIds()).to.deep.equal(['r2']);
    expect(byTestId('gt-select-clear').hidden).to.equal(false);

    byTestId('gt-select-clear').click();
    expect(count()).to.equal('0');
    expect(box.checked).to.equal(false);
    expect(byTestId('gt-select-clear').hidden, 'nothing to clear').to.equal(true);
  });

  it('leaves a tap on the tile itself to the detail: it does not choose the result', async () => {
    await show('gentest-history-idle.html');
    card('r0').querySelector<HTMLElement>('[data-testid="gt-open"]')!.click();
    card('r0').querySelector<HTMLElement>('img')!.click();
    expect(input('r0').checked).to.equal(false);
    expect(count()).to.equal('0');
  });

  it('does not offer a result that has no file yet', async () => {
    await show('gentest-history-running.html');
    expect(statuses()).to.deep.equal(['succeeded', 'succeeded', 'running']);
    expect(host.querySelectorAll('[data-gt-pick]').length).to.equal(2);
    expect(card('r2'), 'a running result is offered').to.equal(null);
  });

  it('keeps the choice when the history refreshes itself and draws the cards again', async function () {
    this.timeout(20_000);
    await show('gentest-history-running.html');
    tapMark('r0');
    const before = card('r0');
    expect(count()).to.equal('1');

    await waitUntil(() => !statuses().includes('running'), 'the finished result did not arrive', { timeout: 8000 });

    expect(card('r0'), 'the cards were not drawn again').not.to.equal(before);
    expect(input('r0').checked, 'the choice was lost by the refresh').to.equal(true);
    expect(input('r1').checked).to.equal(false);
    expect(card('r2'), 'the result that finished is offered now').not.to.equal(null);
    expect(input('r2').checked).to.equal(false);
    expect(count()).to.equal('1');
    expect(hiddenIds()).to.deep.equal(['r0']);
    expect((byTestId('gt-export-selected-submit') as HTMLButtonElement).disabled).to.equal(false);

    tapMark('r2'); // and a tile of the new draw can be chosen too
    expect(hiddenIds().sort()).to.deep.equal(['r0', 'r2']);
  });

  it('does not save a chosen result that the commercial-use filter hides, and the other form follows the filter', async () => {
    await show('gentest-history-idle.html');
    tapMark('r0');
    expect(hiddenIds()).to.deep.equal(['r0']);

    card('r0').closest('li')!.hidden = true; // what the filter does to a card it does not allow
    host.querySelector('#gt-history')!.dispatchEvent(new CustomEvent('model-pick-applied', { bubbles: true }));
    expect(hiddenIds(), 'a hidden card was saved').to.deep.equal([]);
    expect(count()).to.equal('0');

    card('r0').closest('li')!.hidden = false;
    host.querySelector('#gt-history')!.dispatchEvent(new CustomEvent('model-pick-applied', { bubbles: true }));
    expect(hiddenIds(), 'the choice is back with the card').to.deep.equal(['r0']);
  });

  describe('the bar asks the server first, and a choice that cannot be saved is said beside the buttons (#932)', () => {
    const status = () => byTestId('gt-export-status');
    const downloads = async (): Promise<string[]> => (await fetch('/__export-downloads')).json();
    // The form is a plain GET form that htmx takes over: its submit event is what the reader's tap makes.
    const submit = (testId: string) => byTestId(testId).closest('form')!.requestSubmit();

    it('draws the reason in the line of the bar, keeps the page and downloads nothing', async () => {
      await show('gentest-history-idle.html');
      const before = (await downloads()).length;
      (host.querySelector('#gt-export-latest') as HTMLInputElement).value = '7'; // what the answer of the stub refuses

      submit('gt-export-latest-submit');
      await waitUntil(() => status().textContent!.includes('書き出せる結果がありません'), 'the reason was not drawn');

      expect(status().querySelector('[role="alert"]'), 'the reason is an alert').not.to.equal(null);
      expect(byTestId('gt-history'), 'the screen was replaced').not.to.equal(null);
      expect(byTestId('gt-export-latest-submit'), 'the bar is still there').not.to.equal(null);
      expect(await downloads()).to.have.length(before);
    });

    it('clears the reason when the next ask starts and downloads the choice that can be saved', async () => {
      await show('gentest-history-idle.html');
      const before = (await downloads()).length;
      (host.querySelector('#gt-export-latest') as HTMLInputElement).value = '7';
      submit('gt-export-latest-submit');
      await waitUntil(() => status().textContent!.includes('書き出せる'), 'the reason was not drawn');

      tapMark('r1');
      submit('gt-export-selected-submit');
      await waitUntil(() => status().textContent === '', 'the old reason stayed');
      await waitUntil(async () => (await downloads()).length === before + 1, 'the browser did not load the download');

      const url = (await downloads())[before];
      expect(url).to.contain('/generation-test/export?');
      expect(url).to.contain('id=r1');
      expect(byTestId('gt-history'), 'the page stays').not.to.equal(null);
    });
  });

  it('lights the frame of a chosen card with the real CSS and puts it out when it is let go', async function () {
    this.timeout(60_000); // the first request waits for the build of the CSS
    const stylesheet = document.createElement('link');
    stylesheet.rel = 'stylesheet';
    stylesheet.href = '/__tailwind.css';
    await new Promise<void>((resolve, reject) => {
      stylesheet.onload = () => resolve();
      stylesheet.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
      document.head.append(stylesheet);
    });
    // The frame fades in and out (transition-shadow), and a background tab of a headless browser may not advance a transition at all: this test is about the end states, so
    // it is measured without the fade (waiting for it would depend on the frames the browser draws).
    const noFade = document.createElement('style');
    noFade.textContent = '*, *::before, *::after { transition: none !important; }';
    document.head.append(noFade);
    try {
      await show('gentest-history-idle.html');
      const frame = () => card('r0').querySelector<HTMLElement>('input + div')!;
      const mark = () => getComputedStyle(card('r0').querySelector('[data-testid="gt-pick-mark"]')!);
      const unlit = mark().backgroundColor;
      const lit = () => getComputedStyle(frame());
      const plainFace = lit().backgroundColor;
      expect(lit().boxShadow, 'no card glows').to.equal('none');

      tapMark('r0');
      expect(lit().backgroundColor, 'the chosen card is tinted').not.to.equal(plainFace);
      expect(lit().borderTopColor).not.to.equal(getComputedStyle(frame().parentElement!).borderTopColor);
      expect(mark().backgroundColor, 'the mark in the corner is not filled').not.to.equal(unlit);

      tapMark('r0');
      input('r0').blur(); // the ring of the keyboard focus (peer-focus-visible) is another shadow of the frame: whether the label tap leaves the focus-visible look depends on the browser's last input, and this test is about the glow of the choice
      expect(lit().boxShadow).to.equal('none');
    } finally {
      stylesheet.remove();
      noFade.remove();
    }
  });
});
