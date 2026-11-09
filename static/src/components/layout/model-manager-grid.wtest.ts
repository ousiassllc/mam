import { expect } from '@open-wc/testing';
import { setViewport } from '@web/test-runner-commands';

// #1102: every list of cards of the model manager has the same columns, one up to 639 px, two from 640 px and three from 1280 px
// (modelcard.GridClass), and the page never scrolls sideways. #1103: the page of the custom nodes shows no description of a node (it is in
// the detail modal). The markup is what the Go components render (backend/internal/web/mm_grid_fixture_test.go and scroll_fixture_test.go keep it
// current), drawn with the real Tailwind CSS (web-test-runner.config.mjs builds it from src/css/app.css), so this measures the screens' own markup.
const TESTDATA = '/src/components/testdata/';

/** The lists of cards: a page of the model manager (the fixture), the list in it and the cards it has at least. */
const LISTS: { name: string; fixture: string; list: string; cards: number }[] = [
  { name: 'the models', fixture: 'scroll-mm-page.html', list: '[data-testid=mm-models]', cards: 3 },
  {
    name: 'the custom nodes',
    fixture: 'layout-mm-grid-custom-nodes.html',
    list: '[data-testid=mm-nodes] > ul',
    cards: 3,
  },
  {
    name: 'the voice, lip-sync and LLM runtimes',
    fixture: 'layout-mm-grid-local-runtimes.html',
    list: '[data-testid=mm-runtimes] > ul',
    cards: 3,
  },
  {
    name: 'the added files',
    fixture: 'layout-mm-grid-hf-external.html',
    list: '[data-testid=mm-hfi-model-list]',
    cards: 3,
  },
];

const columnsOf = (list: Element) => getComputedStyle(list).gridTemplateColumns.split(' ').filter(Boolean).length;
const sideways = () => document.documentElement.scrollWidth - document.documentElement.clientWidth;

describe('the columns of the lists of cards (#1102)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  /** The page as the app frames it: the body of a category in a `max-w-5xl` main (pagehead.Wide), optionally beside the open side menu. */
  async function show(fixture: string, withMenu = false): Promise<void> {
    const html = await (await fetch(`${TESTDATA}${fixture}`)).text();
    host = document.createElement('div');
    if (withMenu) {
      const nav = await (await fetch(`${TESTDATA}layout-sidenav.html`)).text();
      host.innerHTML = `<div class="flex min-h-screen">${nav}<div id="content" class="min-w-0 flex-1"><main class="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 md:p-8">${html}</main></div></div>`;
    } else {
      host.innerHTML = `<main class="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 md:p-8">${html}</main>`;
    }
    document.body.append(host);
  }

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
  });

  afterEach(() => {
    host?.remove();
    stylesheet.remove();
  });

  const columns: [number, number][] = [
    [639, 1],
    [640, 2],
    [1279, 2],
    [1280, 3],
  ];
  for (const { name, fixture, list, cards } of LISTS) {
    for (const [width, expected] of columns) {
      it(`draws ${name} in ${expected} column(s) at ${width} px without scrolling sideways`, async () => {
        await setViewport({ width, height: 800 });
        await show(fixture);
        const ul = host.querySelector<HTMLElement>(list)!;

        expect(ul, `the fixture has no ${list}`).to.not.equal(null);
        expect(ul.children.length, 'the list has too few cards to show the columns').to.be.at.least(cards);
        expect(columnsOf(ul)).to.equal(expected);
        expect(ul.scrollWidth, 'the list is wider than its box').to.be.at.most(ul.clientWidth);
        expect(sideways(), 'the page scrolls sideways').to.be.at.most(0);
      });
    }

    it(`gives a card of ${name} at least 280 px at 1280 px with the side menu open`, async () => {
      await setViewport({ width: 1280, height: 800 });
      await show(fixture, true);
      const ul = host.querySelector<HTMLElement>(list)!;

      expect(columnsOf(ul)).to.equal(3);
      for (const card of ul.children) {
        expect(card.getBoundingClientRect().width).to.be.at.least(280);
      }
      expect(sideways(), 'the page scrolls sideways').to.be.at.most(0);
    });
  }

  for (const width of [639, 1280]) {
    it(`keeps the engine card at the whole width at ${width} px (the one exception)`, async () => {
      await setViewport({ width, height: 800 });
      await show('layout-long-names-mm-comfyui.html');
      const ul = host.querySelector<HTMLElement>('[data-testid=mm-engine-list]')!;

      expect(columnsOf(ul)).to.equal(1);
      expect(ul.children[0]!.getBoundingClientRect().width).to.be.closeTo(ul.getBoundingClientRect().width, 1);
    });
  }
});

describe('the page of the custom nodes (#1103)', () => {
  let host: HTMLElement;

  afterEach(() => host?.remove());

  it('shows no description of a node: the page is shorter by the descriptions, which the detail modal has', async () => {
    const descriptions: string[] = await (await fetch(`${TESTDATA}layout-mm-grid-node-descriptions.json`)).json();
    const html = await (await fetch(`${TESTDATA}layout-mm-grid-custom-nodes.html`)).text();
    host = document.createElement('div');
    host.innerHTML = html;
    document.body.append(host);
    const text = host.innerText;
    const total = descriptions.reduce((sum, d) => sum + [...d].length, 0);

    expect(descriptions.length).to.equal(5);
    expect(total, 'the descriptions are the text that was taken off the page').to.be.at.least(1200);
    expect(host.querySelectorAll('[data-testid=mm-node]').length).to.equal(5);
    for (const description of descriptions) {
      expect(text).to.not.contain(description.slice(0, 20));
    }
    // What stays: the name, the one line and the state of each card.
    expect(text).to.contain('追加ノード・1 MiB');
  });
});

// #1110: the name of a card is a button whose click area covers the whole card, so a control in the card's body is pressed only when it is raised above it. A click
// at the middle of the button that opens the reason a model cannot be installed (#1118: it replaced the long text and the link) must reach that button, and no other
// control of the card.
describe('the button that opens the reason, on the card of a model that needs a node (#1110, #1118)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

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
  });

  afterEach(() => {
    host?.remove();
    stylesheet.remove();
  });

  it('is what a click at its middle lands on, not the name button that covers the card', async () => {
    await setViewport({ width: 1280, height: 800 });
    const html = await (await fetch(`${TESTDATA}layout-mm-card-node-needed.html`)).text();
    host = document.createElement('div');
    host.innerHTML = `<main class="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 md:p-8">${html}</main>`;
    document.body.append(host);
    const button = host.querySelector<HTMLButtonElement>('[data-testid=mm-reason-open]')!;

    expect(button, 'the fixture has no button of the reason').to.not.equal(null);
    const box = button.getClientRects()[0]!;
    const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
    expect(hit === button || button.contains(hit), 'the button is covered by another element of the card').to.equal(
      true,
    );
  });
});
