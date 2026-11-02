import { expect } from '@open-wc/testing';
import { setViewport } from '@web/test-runner-commands';

// What the real screens put on the page, drawn with the real Tailwind CSS (web-test-runner.config.mjs builds it from
// src/css/app.css) in a phone-width window: the page must not scroll sideways. A wide thing scrolls inside its own box
// or wraps; it never pushes the page wider (#881: the form of "Hugging Face から導入"; #882: the table of /settings/llm). The markup is what the Go
// components render (backend/internal/web/layout_fixture_test.go keeps it current), so this checks the screens' own
// markup.
const TESTDATA = '/src/components/testdata/';

/** The side menu (the fixture) and a page beside it, as layout.Shell puts them; appended to the body. */
async function menuBesidePage(): Promise<HTMLElement> {
  const nav = await (await fetch(TESTDATA + 'layout-sidenav.html')).text();
  const host = document.createElement('div');
  host.innerHTML = `<div class="flex min-h-screen">${nav}<div id="content" class="min-w-0 flex-1"><main data-testid="page" class="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 md:p-8"><h1 class="text-xl font-semibold">Brief</h1><p class="break-words">長い文章が続きます。</p></main></div></div>`;
  document.body.append(host);
  return host;
}

describe('a phone-width window (375 px)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  const page = () => document.documentElement;
  const overflowOf = () => page().scrollWidth - page().clientWidth;

  /** Draws a fixture the way the screens hold it: in a padded column of the page. */
  async function show(fixture: string): Promise<void> {
    const html = await (await fetch(TESTDATA + fixture)).text();
    host = document.createElement('div');
    host.style.padding = '16px';
    host.innerHTML = html;
    document.body.append(host);
  }

  beforeEach(async function () {
    // The first request waits for the build of the CSS (a few seconds on a busy machine).
    this.timeout(60_000);
    stylesheet = document.createElement('link');
    stylesheet.rel = 'stylesheet';
    stylesheet.href = '/__tailwind.css';
    const loaded = new Promise<void>((resolve, reject) => {
      stylesheet.onload = () => resolve();
      stylesheet.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
    });
    document.head.append(stylesheet);
    await loaded;
    await setViewport({ width: 375, height: 800 });
  });

  afterEach(() => {
    host?.remove();
    stylesheet.remove();
  });

  it('keeps the form of "Hugging Face から導入" inside the page', async () => {
    await show('layout-hf-import.html');

    const field = host.querySelector<HTMLInputElement>('#mm-hfi-q')!;
    expect(page().clientWidth).to.equal(375);
    expect(field.getBoundingClientRect().right, 'the URL field is cut by the edge of the page').to.be.at.most(
      page().clientWidth,
    );
    expect(overflowOf(), 'the page scrolls sideways').to.be.at.most(0);
  });

  it('scrolls the table of /settings/llm inside its own box instead of the page', async () => {
    await show('layout-llm-section.html');

    const box = host.querySelector<HTMLElement>('#llm-table')!.closest<HTMLElement>('.overflow-x-auto')!;
    expect(page().clientWidth).to.equal(375);
    expect(box.scrollWidth, 'the table fits: the test no longer shows a wide table').to.be.greaterThan(box.clientWidth);
    expect(overflowOf(), 'the page scrolls sideways').to.be.at.most(0);
  });

  for (const media of ['image', 'refs', 'video']) {
    it(`keeps the generation test form (${media}) with its model cards inside the page`, async () => {
      await show(`layout-gentest-${media}.html`);

      const cards = [...host.querySelectorAll<HTMLElement>('[data-testid=gt-model]')];
      expect(cards.length).to.be.greaterThan(1);
      expect(page().clientWidth).to.equal(375);
      for (const card of cards) {
        expect(card.getBoundingClientRect().right, 'a model card is cut by the edge of the page').to.be.at.most(
          page().clientWidth,
        );
      }
      expect(overflowOf(), 'the page scrolls sideways').to.be.at.most(0);
    });
  }

  it('shows the side menu as icons only beside the page, which keeps the rest of the width (#915)', async () => {
    host = await menuBesidePage();

    const nav = host.querySelector<HTMLElement>('nav')!;
    const page_ = host.querySelector<HTMLElement>('[data-testid=page]')!;
    expect(nav.getBoundingClientRect().width, 'a narrow window folds the menu to its icons').to.be.at.most(64);
    expect(page_.getBoundingClientRect().right, 'the page is cut by the edge of the window').to.be.at.most(
      page().clientWidth,
    );
    expect(overflowOf(), 'the page scrolls sideways').to.be.at.most(0);
    const label = host.querySelector<HTMLElement>('[data-key=brief] span:last-child')!;
    expect(
      label.getBoundingClientRect().width,
      'the label is not drawn but is still there for a screen reader',
    ).to.be.at.most(1);
    expect(label.textContent).to.equal('Brief');
  });
});

describe('a desktop window (1280 px and 1440 px) (#915)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  beforeEach(async function () {
    this.timeout(60_000);
    stylesheet = document.createElement('link');
    stylesheet.rel = 'stylesheet';
    stylesheet.href = '/__tailwind.css';
    const loaded = new Promise<void>((resolve, reject) => {
      stylesheet.onload = () => resolve();
      stylesheet.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
    });
    document.head.append(stylesheet);
    await loaded;
    host = await menuBesidePage();
  });

  afterEach(() => {
    host.remove();
    stylesheet.remove();
  });

  for (const width of [1280, 1440]) {
    it(`draws the open menu at 14 rem and folds it to 3.5 rem at ${width} px, the page taking the rest`, async () => {
      await setViewport({ width, height: 800 });
      const nav = host.querySelector<HTMLElement>('nav')!;
      const page_ = host.querySelector<HTMLElement>('[data-testid=page]')!;
      const label = host.querySelector<HTMLElement>('[data-key=brief] span:last-child')!;

      expect(nav.getBoundingClientRect().width).to.equal(224);
      expect(label.getBoundingClientRect().width, 'the open menu draws the label').to.be.greaterThan(8);
      expect(page_.getBoundingClientRect().right).to.be.at.most(width);

      nav.dataset.folded = 'true';
      expect(nav.getBoundingClientRect().width).to.equal(56);
      expect(label.getBoundingClientRect().width, 'the folded menu keeps only the icon').to.be.at.most(1);
      expect(host.querySelector<HTMLElement>('#content')!.getBoundingClientRect().width).to.be.greaterThan(
        width - 56 - 1,
      );
      expect(document.documentElement.scrollWidth - document.documentElement.clientWidth).to.be.at.most(0);
    });
  }
});
