import { expect } from '@open-wc/testing';
import { setViewport } from '@web/test-runner-commands';

// #1138: the chat of the single generation in a phone-width window (375 px) and a desktop one, drawn with the real Tailwind CSS: the page must not scroll
// sideways. The long names of a model and the table of the breakdown scroll in their own box or wrap; they never push the page wider. The markup is what
// the Go components render (backend/internal/web/organisms/gentest/chat/fixture_test.go).
describe('the chat of the single generation in a narrow window (#1138)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;
  const original = { width: window.innerWidth, height: window.innerHeight };

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
    host = document.createElement('div');
    host.style.padding = '16px';
    host.innerHTML = await (await fetch('/src/components/testdata/gentest-chat-thread.html')).text();
    document.body.append(host);
    const panel = host.querySelector('section[data-gtc]')!;
    host.querySelector('#gtc-thread')!.append(...[...host.children].filter((el) => el !== panel));
  });

  afterEach(async () => {
    host?.remove();
    stylesheet?.remove();
    await setViewport(original); // the window is shared with the other test files of the run
  });

  for (const width of [375, 1280]) {
    it(`keeps the thread, the card and the results inside the page at ${width} px`, async () => {
      await setViewport({ width, height: 900 });
      const page = document.documentElement;
      expect(window.innerWidth, 'the viewport was set').to.equal(width); // not clientWidth: a scrollbar takes some of it
      expect(page.scrollWidth - page.clientWidth, 'the page scrolls sideways').to.be.at.most(0);
      for (const id of ['gtc-proposal', 'gtc-result', 'gtc-composer']) {
        const el = host.querySelector<HTMLElement>(`[data-testid="${id}"]`)!;
        expect(el.getBoundingClientRect().right, `${id} is cut by the edge of the page`).to.be.at.most(
          page.clientWidth,
        );
      }
      const run = host.querySelector<HTMLElement>('[data-testid="gtc-run"]')!;
      expect(run.getBoundingClientRect().height, 'the run button is a touch target').to.be.at.least(36);
    });
  }
});
