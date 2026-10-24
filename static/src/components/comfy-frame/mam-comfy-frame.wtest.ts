import { expect, oneEvent, waitUntil } from '@open-wc/testing';
import './mam-comfy-frame';
import type { MamComfyFrame } from './mam-comfy-frame';

// A same-origin page stands for ComfyUI: the dev server serves /src/components/testdata/ and answers 404 to
// the others, and a frame that never loads is an address nothing listens on.
const LOADS = '/src/components/testdata/focus-lock-after.html';
const NEVER = 'http://127.0.0.1:9/never';

async function frame(attrs: string): Promise<MamComfyFrame> {
  const host = document.createElement('div');
  host.innerHTML = `<mam-comfy-frame ${attrs}></mam-comfy-frame>`;
  document.body.append(host);
  const el = host.firstElementChild as MamComfyFrame;
  await el.updateComplete;
  return el;
}

describe('mam-comfy-frame', () => {
  afterEach(() => {
    for (const el of document.querySelectorAll('mam-comfy-frame')) el.parentElement?.remove();
  });

  it('shows the frame and the open link, and no failure, once the page loaded', async () => {
    const el = await frame(`src="${LOADS}" open-url="http://127.0.0.1:8188/" timeout-ms="5000"`);
    await waitUntil(() => el.frameState === 'loaded', 'the frame did not load', { timeout: 3000 });

    expect(el.querySelector('iframe')?.getAttribute('src')).to.equal(LOADS);
    expect(el.querySelector('[data-testid="comfy-frame-failure"]')).to.equal(null);
    const link = el.querySelector<HTMLAnchorElement>('[data-testid="comfy-frame-open"]');
    expect(link?.getAttribute('href')).to.equal('http://127.0.0.1:8188/');
    expect(link?.getAttribute('target')).to.equal('_blank');
    expect(link?.getAttribute('rel')).to.equal('noopener noreferrer');
  });

  it('gives up when the frame has not loaded in time: the message and the link replace the frame', async () => {
    // A frame whose `load` never fires: an unconnected iframe is simulated by a src the page cannot load in time.
    const el = await frame(
      `src="${NEVER}" open-url="http://127.0.0.1:8188/" failure-message="ComfyUI を開けません" timeout-ms="50"`,
    );
    el.querySelector('iframe')?.addEventListener('load', (e) => e.stopImmediatePropagation(), true);
    const failed = oneEvent(el, 'comfy-frame-failed');
    await failed;
    await el.updateComplete;

    expect(el.frameState).to.equal('failed');
    expect(el.querySelector('[data-testid="comfy-frame-failure"]')?.textContent).to.contain('ComfyUI を開けません');
    expect(el.querySelector('iframe')).to.equal(null);
    expect(el.querySelector('[data-testid="comfy-frame-open"]')).to.not.equal(null);
  });

  it('shows the failure at once when the frame reports an error', async () => {
    const el = await frame(`src="${LOADS}" failure-message="だめでした" timeout-ms="60000"`);
    el.querySelector('iframe')?.dispatchEvent(new Event('error'));
    await el.updateComplete;

    expect(el.frameState).to.equal('failed');
    expect(el.querySelector('[data-testid="comfy-frame-failure"]')?.textContent).to.contain('だめでした');
  });

  it('starts again with a new src after it failed', async () => {
    const el = await frame(`src="${LOADS}" timeout-ms="60000"`);
    el.querySelector('iframe')?.dispatchEvent(new Event('error'));
    await el.updateComplete;
    expect(el.frameState).to.equal('failed');

    el.setAttribute('src', `${LOADS}?again`);
    await el.updateComplete;
    await waitUntil(() => el.frameState === 'loaded', 'the new frame did not load', { timeout: 3000 });
    expect(el.querySelector('[data-testid="comfy-frame-failure"]')).to.equal(null);
  });

  it('draws no frame without a src', async () => {
    const el = await frame(`open-url="http://127.0.0.1:8188/"`);
    expect(el.querySelector('iframe')).to.equal(null);
    expect(el.querySelector('[data-testid="comfy-frame-open"]')).to.not.equal(null);
  });
});
