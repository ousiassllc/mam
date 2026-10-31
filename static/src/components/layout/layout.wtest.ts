import { expect } from '@open-wc/testing';
import { setViewport } from '@web/test-runner-commands';

// What the real screens put on the page, drawn with the real Tailwind CSS (web-test-runner.config.mjs builds it from
// src/css/app.css) in a phone-width window: the page must not scroll sideways. A wide thing scrolls inside its own box
// or wraps; it never pushes the page wider (#881: the form of "Hugging Face から導入"). The markup is what the Go
// components render (backend/internal/web/layout_fixture_test.go keeps it current), so this checks the screens' own
// markup.
const TESTDATA = '/src/components/testdata/';

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
});
