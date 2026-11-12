import { expect, fixture, html, waitUntil } from '@open-wc/testing';
import { sendKeys } from '@web/test-runner-commands';
import './mam-mask-paint';
import type { MamMaskPaint } from './mam-mask-paint';
import { meanLuminance, overlayToken } from './mask-draw';
import { loadTailwind } from '../testdata/tailwind';
import { stubConfirm } from '../confirm/testkit';
import { clearButton, flat, paint, pixel } from '../testdata/mask-kit';

describe('against lit-widgets-guide §5 (#1093)', () => {
  let tailwind: { restore(): void };
  beforeEach(async function () {
    this.timeout(60_000); // the first request waits for the build of the CSS
    tailwind = await loadTailwind();
  });
  afterEach(() => tailwind.restore());

  it('says so, and draws no canvas, while there is no picture (MP-7)', async () => {
    const el = await fixture<MamMaskPaint>(html`<mam-mask-paint></mam-mask-paint>`);
    const empty = el.querySelector<HTMLElement>('[data-empty]')!;
    expect(empty.textContent).to.equal('画像を選ぶと、描き直す部分を塗れます');
    expect(el.querySelector('canvas')).to.equal(null);
    expect(getComputedStyle(empty).borderTopStyle).to.equal('dashed');
    expect(parseFloat(getComputedStyle(empty).paddingTop)).to.equal(24); // p-6
    expect(el.querySelector('[aria-label="描き方"]'), 'the tools stay').to.not.equal(null);

    el.src = flat(32); // a picture is chosen: the canvas replaces the empty state
    await waitUntil(() => el.querySelector('canvas')?.width === 32, 'the picture was not read');
    expect(el.querySelector('[data-empty]')).to.equal(null);
  });

  it('turns 全部消す off while there is no picture, so it opens no question that does nothing (W10)', async () => {
    const el = await fixture<MamMaskPaint>(html`<mam-mask-paint></mam-mask-paint>`);
    const clear = clearButton(el);
    expect(clear.disabled).to.equal(true);
    expect(parseFloat(getComputedStyle(clear).opacity), 'drawn dimmed').to.be.lessThan(1);
    const ask = stubConfirm(true);
    try {
      clear.click(); // a disabled button gets no click: the question is not asked
      await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      expect(ask.messages).to.deep.equal([]);
    } finally {
      ask.restore();
    }

    el.src = flat(32); // a picture is chosen: the button works
    await waitUntil(() => el.querySelector('canvas')?.width === 32, 'the picture was not read');
    expect(clearButton(el).disabled).to.equal(false);
  });

  it('still exports a black mask without a picture, so a form can be sent', async () => {
    const el = await fixture<MamMaskPaint>(html`<mam-mask-paint width="32" height="32"></mam-mask-paint>`);
    const bmp = await createImageBitmap(await el.maskPNG());
    expect([bmp.width, bmp.height]).to.deep.equal([32, 32]);
    expect((await pixel(await el.maskPNG(), 5, 5)).r).to.equal(0);
  });

  it('shows the thickness as a number beside the slider and keeps it up to date (MP-2)', async () => {
    const el = await paint();
    const output = el.querySelector<HTMLOutputElement>('output')!;
    expect(output.textContent).to.equal('16');
    expect(getComputedStyle(output).fontFamily).to.match(/mono/i);
    expect(getComputedStyle(output).fontVariantNumeric).to.contain('tabular-nums');

    el.querySelector('canvas')!.dispatchEvent(new KeyboardEvent('keydown', { key: ']', bubbles: true }));
    await el.updateComplete;
    expect(output.textContent, 'by the key').to.equal('18');

    const slider = el.querySelector<HTMLInputElement>('input[type="range"]')!;
    slider.value = '40';
    slider.dispatchEvent(new Event('input', { bubbles: true }));
    await el.updateComplete;
    expect(output.textContent, 'by the slider').to.equal('40');
    expect(slider.value).to.equal('40');
  });

  it('draws the tool buttons as small sub buttons with a ripple, and tells the shortcuts (MP-1, MP-5)', async () => {
    const el = await paint();
    const [brush, eraser] = [...el.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')];
    for (const button of el.querySelectorAll<HTMLButtonElement>('[aria-label="描き方"] button')) {
      expect(button.getBoundingClientRect().height, button.textContent!).to.be.at.least(32);
      expect(parseFloat(getComputedStyle(button).fontSize)).to.be.at.least(14);
      expect(button.hasAttribute('data-ripple'), button.textContent!).to.equal(true);
      expect(button.className).to.contain('motion-reduce:transition-none');
    }
    expect(brush!.getAttribute('aria-keyshortcuts')).to.equal('B');
    expect(eraser!.getAttribute('aria-keyshortcuts')).to.equal('E');
    expect(clearButton(el).hasAttribute('aria-pressed'), '全部消す is not a tool').to.equal(false);
    // The chosen tool has the accent border; the other has the plain one.
    expect(getComputedStyle(brush!).borderTopColor).to.not.equal(getComputedStyle(eraser!).borderTopColor);
  });

  it('shows the focus ring around the picture while the canvas has the keyboard focus (MP-4)', async () => {
    const el = await paint();
    const stage = el.querySelector<HTMLElement>('[data-stage]')!;
    expect(getComputedStyle(stage).outlineStyle, 'no ring before the focus').to.equal('none');

    el.querySelector<HTMLCanvasElement>('canvas')!.focus();
    await sendKeys({ press: 'Shift' }); // :focus-visible matches after a key press
    expect(el.querySelector('canvas')!.matches(':focus-visible')).to.equal(true);
    // On the frame, not on the canvas: the canvas is drawn at .55 opacity, which would dim its own ring.
    expect(getComputedStyle(stage).outlineStyle).to.equal('solid');
    expect(getComputedStyle(stage).outlineWidth).to.equal('2px');
    expect(getComputedStyle(el.querySelector('canvas')!).outlineStyle, 'the canvas has no ring of its own').to.equal(
      'none',
    );
  });

  it('wraps the frame (and the ring) around the picture, so the overlay lies exactly over it (MP-3, MP-4)', async () => {
    const el = await paint();
    const stage = el.querySelector<HTMLElement>('[data-stage]')!.getBoundingClientRect();
    const picture = el.querySelector<HTMLElement>('canvas[data-picture]')!.getBoundingClientRect();
    const view = el.querySelector('canvas')!.getBoundingClientRect();
    expect(el.getBoundingClientRect().width, 'there is room to stretch').to.be.greaterThan(picture.width + 100);
    expect([stage.width, stage.height]).to.deep.equal([picture.width, picture.height]);
    expect([view.width, view.height]).to.deep.equal([picture.width, picture.height]);
  });

  it('paints the overlay in the --overlay-deep token over a bright picture, not in a colour of its own (MP-3)', async () => {
    const el = await fixture<MamMaskPaint>(html`<mam-mask-paint src=${flat(32, '#e6dccb')}></mam-mask-paint>`);
    el.style.setProperty('--overlay-deep', 'rgb(1, 2, 3)');
    el.style.setProperty('--overlay-pale', 'rgb(250, 250, 250)');
    await waitUntil(() => el.querySelector('canvas')?.width === 32, 'the picture was not read');
    const view = el.querySelector('canvas')!;
    view.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    const dab = [...view.getContext('2d')!.getImageData(16, 16, 1, 1).data];
    expect(dab, 'the brush').to.deep.equal([1, 2, 3, 255]);
    expect(getComputedStyle(view).opacity, 'the overlay is shown at .55').to.equal('0.55');
    expect((await pixel(await el.maskPNG(), 20, 16)).r, 'the mask itself stays white').to.be.greaterThan(200);
  });

  it('paints the overlay in the --overlay-pale token over a dark picture (MP-3)', async () => {
    const el = await fixture<MamMaskPaint>(html`<mam-mask-paint src=${flat(32, '#101010')}></mam-mask-paint>`);
    el.style.setProperty('--overlay-deep', 'rgb(1, 2, 3)');
    el.style.setProperty('--overlay-pale', 'rgb(250, 240, 230)');
    await waitUntil(() => el.querySelector('canvas')?.width === 32, 'the picture was not read');
    const view = el.querySelector('canvas')!;
    view.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    expect([...view.getContext('2d')!.getImageData(16, 16, 1, 1).data]).to.deep.equal([250, 240, 230, 255]);
  });

  it('chooses the token by the picture brightness: a mid-grey is on the deep side, the pale one starts below a grey of 96', () => {
    const lum = (colour: string): number | null => {
      const c = document.createElement('canvas');
      c.width = 8;
      c.height = 8;
      const g = c.getContext('2d')!;
      g.fillStyle = colour;
      g.fillRect(0, 0, 8, 8);
      return meanLuminance(c);
    };
    expect(overlayToken(lum('#000'))).to.equal('--overlay-pale');
    expect(overlayToken(lum('#404040'))).to.equal('--overlay-pale');
    expect(overlayToken(lum('#808080'))).to.equal('--overlay-deep');
    expect(overlayToken(lum('#fff'))).to.equal('--overlay-deep');
    expect(overlayToken(null), 'a picture that cannot be read').to.equal('--overlay-deep');
    expect(lum('#fff')).to.be.closeTo(1, 0.01);
  });

  it('draws the mask to start from in the overlay colour too (MP-3)', async () => {
    const mask = document.createElement('canvas');
    mask.width = 32;
    mask.height = 32;
    const g = mask.getContext('2d')!;
    g.fillStyle = '#000';
    g.fillRect(0, 0, 32, 32);
    g.fillStyle = '#fff';
    g.fillRect(0, 0, 16, 32);
    const el = await fixture<MamMaskPaint>(
      html`<mam-mask-paint src=${flat(32)} mask-src=${mask.toDataURL('image/png')}></mam-mask-paint>`,
    );
    el.style.setProperty('--overlay-deep', 'rgb(9, 8, 7)'); // before the images are read
    await waitUntil(async () => (await pixel(await el.maskPNG(), 4, 16)).r > 200, 'the mask was not drawn');
    const view = el.querySelector('canvas')!.getContext('2d')!;
    expect([...view.getImageData(4, 16, 1, 1).data], 'the white part').to.deep.equal([9, 8, 7, 255]);
    expect(view.getImageData(28, 16, 1, 1).data[3], 'the black part').to.equal(0);
  });
});
