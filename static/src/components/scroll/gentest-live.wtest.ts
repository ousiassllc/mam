import { expect, waitUntil } from '@open-wc/testing';
import { setViewport } from '@web/test-runner-commands';
import htmx from 'htmx.org';
import '../../main';

// #940: the grid of a running batch (BatchBox) asks for itself every 2 s and the answer replaces the whole section, so the
// scrolling box of the grid was made again at the left end and a reader who had scrolled right to look at the images on the
// right was thrown back. The page is the real markup (backend/internal/web/gentest_live_fixture_test.go keeps the files
// current) drawn with the real Tailwind CSS; the answers of the poll are what the handler renders.
const TESTDATA = '/src/components/testdata/';

const frames = () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));

async function loadTailwind(): Promise<HTMLLinkElement> {
  const stylesheet = document.createElement('link');
  stylesheet.rel = 'stylesheet';
  stylesheet.href = '/__tailwind.css';
  const loaded = new Promise<void>((resolve, reject) => {
    stylesheet.onload = () => resolve();
    stylesheet.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
  });
  document.head.append(stylesheet);
  await loaded;
  return stylesheet;
}

describe('the grid of a running batch keeps the place the reader scrolled to (#940)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  const byTestId = (id: string) => host.querySelector<HTMLElement>(`[data-testid="${id}"]`)!;
  const scroller = () => byTestId('gt-grid').parentElement!;

  beforeEach(async function () {
    this.timeout(60_000); // the first request waits for the build of the CSS
    stylesheet = await loadTailwind();
    await setViewport({ width: 1280, height: 800 });
    host = document.createElement('div');
    host.className = 'mx-auto max-w-5xl p-6';
    host.innerHTML = await (await fetch(`${TESTDATA}gentest-batch-running.html`)).text();
    document.body.append(host);
    htmx.process(host);
  });

  afterEach(() => {
    host.remove();
    stylesheet.remove();
  });

  it('keeps the grid where it was scrolled to when the progress is drawn again', async function () {
    this.timeout(20_000);
    const section = byTestId('gt-batch');
    const box = scroller();
    expect(box.scrollWidth, 'the test no longer shows a grid that scrolls sideways').to.be.greaterThan(
      box.clientWidth + 400,
    );

    box.scrollLeft = box.scrollWidth;
    const scrolled = box.scrollLeft;
    expect(scrolled).to.be.greaterThan(400);

    // The section asks for itself every 2 s; the answer is the batch with more results done.
    await waitUntil(
      () => byTestId('gt-batch-progress').textContent?.includes('5 / 16'),
      'the progress did not arrive',
      {
        timeout: 8000,
      },
    );
    await frames();

    expect(byTestId('gt-batch'), 'the section was not drawn again').not.to.equal(section);
    expect(scroller(), 'the scrolling box was not made again').not.to.equal(box);
    expect(scroller().scrollLeft, 'the grid went back from where the reader had scrolled it').to.equal(scrolled);
  });

  it('keeps a grid that was scrolled to the middle in the middle, and one at the right end at the right end', async function () {
    this.timeout(30_000);
    const box = scroller();
    const middle = Math.floor((box.scrollWidth - box.clientWidth) / 2);
    box.scrollLeft = middle;
    expect(box.scrollLeft).to.equal(middle);

    await waitUntil(
      () => byTestId('gt-batch-progress').textContent?.includes('5 / 16'),
      'the progress did not arrive',
      {
        timeout: 8000,
      },
    );
    await frames();
    expect(scroller().scrollLeft, 'a reader in the middle was moved').to.equal(middle);

    // The answer of the poll is always the same page; draw the first one again to see a second swap with the reader at the end.
    host.innerHTML = await (await fetch(`${TESTDATA}gentest-batch-running.html`)).text();
    htmx.process(host);
    const end = scroller();
    end.scrollLeft = end.scrollWidth;
    const max = end.scrollLeft;
    await waitUntil(
      () => byTestId('gt-batch-progress').textContent?.includes('5 / 16'),
      'the progress did not arrive',
      {
        timeout: 8000,
      },
    );
    await frames();
    expect(scroller().scrollLeft, 'a reader at the right end was moved away from it').to.equal(max);
  });

  it('does not put a grid that was not scrolled anywhere but its left end', async function () {
    this.timeout(20_000);
    await waitUntil(
      () => byTestId('gt-batch-progress').textContent?.includes('5 / 16'),
      'the progress did not arrive',
      {
        timeout: 8000,
      },
    );
    await frames();

    expect(scroller().scrollLeft).to.equal(0);
  });
});
