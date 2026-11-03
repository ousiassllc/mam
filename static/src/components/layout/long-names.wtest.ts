import { expect } from '@open-wc/testing';
import { setViewport } from '@web/test-runner-commands';

// #1003: a long file name or model name stays inside its card. The model manager's install progress line
// ("Wan2.2_Remix_….safetensors（1/4）（0%） 107 MiB / 33.1 GiB") came out of the card's right edge. The markup is what the Go
// components render (backend/internal/web/long_names_fixture_test.go keeps it current), drawn with the real Tailwind CSS
// (web-test-runner.config.mjs builds it from src/css/app.css), so this measures the screens' own markup.
const TESTDATA = '/src/components/testdata/';

/** The boxes that hold a name: a card, a row, a job. Nothing in one may be wider than the box itself. */
const BOXES = [
  '[data-card]',
  '[data-testid=mm-hfi-job]',
  '[data-testid=mm-hfi-file]',
  '[data-testid=mm-hfi-hits] > li',
  '[data-testid=jobs-item]',
];

describe('long names stay inside their boxes (#1003)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  const page = () => document.documentElement;

  async function show(fixture: string): Promise<void> {
    const html = await (await fetch(TESTDATA + fixture)).text();
    host = document.createElement('div');
    host.className = 'mx-auto max-w-5xl p-4 md:p-8';
    host.innerHTML = html;
    document.body.append(host);
  }

  /** The boxes whose content reaches out of them (scrollWidth counts what overflows an element without a scroll box). */
  function overflowing(): string[] {
    const found: string[] = [];
    for (const box of host.querySelectorAll<HTMLElement>(BOXES.join(','))) {
      if (box.scrollWidth > box.clientWidth + 1) {
        found.push(`${box.getAttribute('data-testid') ?? box.tagName} (${box.scrollWidth} > ${box.clientWidth})`);
      }
    }
    return found;
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

  for (const width of [375, 1280]) {
    const screens: [string, string][] = [
      ['the model manager (install progress, files, Hugging Face)', 'layout-long-names-model-manager.html'],
      ['the job list', 'layout-long-names-jobs.html'],
    ];
    for (const [name, fixture] of screens) {
      it(`keeps ${name} inside its boxes at ${width} px`, async () => {
        await setViewport({ width, height: 800 });
        await show(fixture);

        expect(host.querySelectorAll(BOXES.join(',')).length, 'the fixture has no box to measure').to.be.greaterThan(0);
        expect(overflowing(), 'a long name comes out of its box').to.deep.equal([]);
        expect(page().scrollWidth - page().clientWidth, 'the page scrolls sideways').to.be.at.most(0);
      });
    }
  }

  it('shows the whole progress line of an install: the file name wraps instead of being cut', async () => {
    await setViewport({ width: 375, height: 800 });
    await show('layout-long-names-model-manager.html');

    const line = host.querySelector<HTMLElement>('[data-model] [data-testid=mm-progress] p')!;
    expect(line.textContent).to.contain('Wan2.2_Remix_NSFW_t2v_14b_high_lighting_v2.0.safetensors（1/4）');
    expect(line.scrollWidth, 'the line is wider than its card').to.be.at.most(line.clientWidth + 1);
  });
});
