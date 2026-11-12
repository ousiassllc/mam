import { expect, fixture, html, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import './mam-mask-paint';
import type { MamMaskPaint } from './mam-mask-paint';
import { stubConfirm } from '../confirm/testkit';
import { clearButton, flat, paint, pixel } from '../testdata/mask-kit';

describe('the inpaint mask painter (spec/components/gentest-screen.md)', () => {
  it('exports white where the brush painted and black elsewhere', async () => {
    const el = await paint();
    const canvas = el.querySelector('canvas')!;
    for (let i = 0; i < 6; i++) canvas.dispatchEvent(new KeyboardEvent('keydown', { key: '[', bubbles: true }));
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    const blob = await el.maskPNG();
    const white = await pixel(blob, 20, 16);
    const black = await pixel(blob, 0, 0);
    expect(white.r).to.be.greaterThan(200);
    expect(white.g).to.be.greaterThan(200);
    expect(white.b).to.be.greaterThan(200);
    expect(black.r).to.equal(0);
    expect(black.g).to.equal(0);
    expect(black.b).to.equal(0);
  });

  it('switches tools from the keyboard and the eraser paints black', async () => {
    const el = await paint();
    const canvas = el.querySelector('canvas')!;
    for (let i = 0; i < 6; i++) canvas.dispatchEvent(new KeyboardEvent('keydown', { key: '[', bubbles: true }));
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'b', bubbles: true }));
    await el.updateComplete;
    expect(el.querySelector('button[aria-pressed]')?.getAttribute('aria-pressed')).to.equal('true');
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'e', bubbles: true }));
    await el.updateComplete;
    expect(el.querySelectorAll('button[aria-pressed]')[1]?.getAttribute('aria-pressed')).to.equal('true');
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    const back = await pixel(await el.maskPNG(), 20, 16);
    expect(back.r).to.equal(0);
  });

  it('clears the mask to black', async () => {
    const el = await paint();
    el.querySelector('canvas')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    const ask = stubConfirm(true);
    try {
      clearButton(el).click();
      await waitUntil(() => ask.messages.length === 1, 'the question was not asked');
      await waitUntil(async () => (await pixel(await el.maskPNG(), 16, 16)).r === 0, 'the mask was not cleared');
      expect(ask.messages).to.deep.equal(['塗った部分をすべて消しますか？']);
    } finally {
      ask.restore();
    }
  });

  it('keeps the painting when the question before 全部消す is answered no (W10)', async () => {
    const el = await paint();
    el.querySelector('canvas')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    const ask = stubConfirm(false);
    try {
      clearButton(el).click();
      await waitUntil(() => ask.messages.length === 1, 'the question was not asked');
      await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))); // the answer was delivered
      expect((await pixel(await el.maskPNG(), 16, 16)).r, 'still painted').to.be.greaterThan(200);
    } finally {
      ask.restore();
    }
  });

  it('keeps a fast drag as one connected stroke', async () => {
    const el = await paint();
    const canvas = el.querySelector('canvas')!;
    for (let i = 0; i < 6; i++) canvas.dispatchEvent(new KeyboardEvent('keydown', { key: '[', bubbles: true }));
    const r = canvas.getBoundingClientRect();
    const at = (type: string, x: number, y: number) =>
      canvas.dispatchEvent(
        new PointerEvent(type, { clientX: r.left + x, clientY: r.top + y, pointerId: 7, bubbles: true }),
      );
    at('pointerdown', 2, 16);
    at('pointermove', 30, 16);
    at('pointerup', 30, 16);
    const blob = await el.maskPNG();
    for (const x of [2, 9, 16, 23, 30]) expect((await pixel(blob, x, 16)).r, `x=${x}`).to.be.greaterThan(200);
    expect((await pixel(blob, 16, 8)).r).to.equal(0);
  });

  it('draws nothing after the pointer is released and keeps the keyboard cursor inside the picture', async () => {
    const el = await paint();
    const canvas = el.querySelector('canvas')!;
    for (let i = 0; i < 6; i++) canvas.dispatchEvent(new KeyboardEvent('keydown', { key: '[', bubbles: true }));
    const r = canvas.getBoundingClientRect();
    canvas.dispatchEvent(new PointerEvent('pointerup', { pointerId: 7, bubbles: true }));
    canvas.dispatchEvent(
      new PointerEvent('pointermove', { clientX: r.left + 28, clientY: r.top + 4, pointerId: 7, bubbles: true }),
    );
    for (let i = 0; i < 10; i++)
      canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    const blob = await el.maskPNG();
    expect((await pixel(blob, 28, 4)).r).to.equal(0);
    expect((await pixel(blob, 3, 16)).r).to.be.greaterThan(200);
  });

  describe('the size of the canvases (#999)', () => {
    /** A flat picture of `w` × `h` as a PNG data URL: its natural size is what the painter reads. */
    function picture(w: number, h: number, draw?: (g: CanvasRenderingContext2D) => void): string {
      const c = document.createElement('canvas');
      c.width = w;
      c.height = h;
      const g = c.getContext('2d')!;
      g.fillStyle = '#336699';
      g.fillRect(0, 0, w, h);
      draw?.(g);
      return c.toDataURL('image/png');
    }

    it('paints a picture within the limit on canvases of its own size', async () => {
      const el = await fixture<MamMaskPaint>(html`<mam-mask-paint src=${picture(320, 200)}></mam-mask-paint>`);
      await waitUntil(() => el.querySelector('canvas')!.width === 320, 'the picture was not read');
      expect(el.querySelector('canvas')!.height).to.equal(200);
      const shown = el.querySelector<HTMLCanvasElement>('canvas[data-picture]')!;
      expect([shown.width, shown.height], 'the picture is shown at its own size').to.deep.equal([320, 200]);
      const bmp = await createImageBitmap(await el.maskPNG());
      expect([bmp.width, bmp.height], 'the mask is the size of the picture').to.deep.equal([320, 200]);
    });

    it('scales a picture over the limit down to the limit on the longer side, and exports a mask of that size', async () => {
      const el = await fixture<MamMaskPaint>(html`<mam-mask-paint src=${picture(4200, 1050)}></mam-mask-paint>`);
      await waitUntil(() => el.querySelector('canvas')!.width === 2048, 'the picture was not read');
      const view = el.querySelector('canvas')!;
      expect([view.width, view.height], 'the view is scaled').to.deep.equal([2048, 512]);
      const bmp = await createImageBitmap(await el.maskPNG());
      expect([bmp.width, bmp.height], 'the mask is scaled').to.deep.equal([2048, 512]);
    });

    it('shows a picture over the limit as a copy scaled down to the limit, not as the picture itself', async () => {
      const src = picture(4200, 1050, (g) => {
        g.fillStyle = '#ff0000';
        g.fillRect(0, 0, 2100, 1050);
      });
      const el = await fixture<MamMaskPaint>(html`<mam-mask-paint src=${src}></mam-mask-paint>`);
      await waitUntil(() => el.querySelector('canvas')!.width === 2048, 'the picture was not read');
      expect(el.querySelectorAll('img').length, 'an <img> would keep the picture decoded at its own size').to.equal(0);
      const shown = el.querySelector<HTMLCanvasElement>('canvas[data-picture]')!;
      expect([shown.width, shown.height], 'the shown picture is scaled').to.deep.equal([2048, 512]);
      const g = shown.getContext('2d')!;
      expect([...g.getImageData(10, 256, 1, 1).data], 'the left half').to.deep.equal([0xff, 0, 0, 0xff]);
      expect([...g.getImageData(2040, 256, 1, 1).data], 'the right half').to.deep.equal([0x33, 0x66, 0x99, 0xff]);
    });

    it('still paints where the pointer is on a scaled canvas', async () => {
      const el = await fixture<MamMaskPaint>(html`<mam-mask-paint src=${picture(4096, 4096)}></mam-mask-paint>`);
      await waitUntil(() => el.querySelector('canvas')!.width === 2048, 'the picture was not read');
      const canvas = el.querySelector('canvas')!;
      const r = canvas.getBoundingClientRect();
      const at = (type: string, fx: number, fy: number) =>
        canvas.dispatchEvent(
          new PointerEvent(type, {
            clientX: r.left + r.width * fx,
            clientY: r.top + r.height * fy,
            pointerId: 3,
            bubbles: true,
          }),
        );
      at('pointerdown', 0.5, 0.5);
      at('pointerup', 0.5, 0.5);
      const blob = await el.maskPNG();
      expect((await pixel(blob, 1024, 1024)).r, 'the middle was painted').to.be.greaterThan(200);
      expect((await pixel(blob, 100, 100)).r).to.equal(0);
    });
  });

  describe('a mask to start from (mask-src, "この設定でもう一度")', () => {
    /** A picture of 64 × 64 that is white on its left half and black on the right. */
    function halfMask(): string {
      const c = document.createElement('canvas');
      c.width = 64;
      c.height = 64;
      const g = c.getContext('2d')!;
      g.fillStyle = '#000';
      g.fillRect(0, 0, 64, 64);
      g.fillStyle = '#fff';
      g.fillRect(0, 0, 32, 64);
      return c.toDataURL('image/png');
    }
    const source = (): string => {
      const c = document.createElement('canvas');
      c.width = 64;
      c.height = 64;
      return c.toDataURL('image/png');
    };

    it('draws it on the mask and shows its white part in red over the picture', async () => {
      const el = await fixture<MamMaskPaint>(
        html`<mam-mask-paint src=${source()} mask-src=${halfMask()}></mam-mask-paint>`,
      );
      await waitUntil(async () => (await pixel(await el.maskPNG(), 8, 32)).r > 200, 'the mask was not drawn');
      const blob = await el.maskPNG();
      expect((await pixel(blob, 56, 32)).r, 'black stays black').to.equal(0);
      const view = el.querySelector('canvas')!.getContext('2d')!;
      expect(view.getImageData(8, 32, 1, 1).data[3], 'the white part is red').to.be.greaterThan(200);
      expect(view.getImageData(56, 32, 1, 1).data[3], 'the black part is clear').to.equal(0);
    });

    it('is cleared by 全部消す like a mask that was painted', async () => {
      const el = await fixture<MamMaskPaint>(
        html`<mam-mask-paint src=${source()} mask-src=${halfMask()}></mam-mask-paint>`,
      );
      await waitUntil(async () => (await pixel(await el.maskPNG(), 8, 32)).r > 200, 'the mask was not drawn');
      const ask = stubConfirm(true);
      try {
        clearButton(el).click();
        await waitUntil(async () => (await pixel(await el.maskPNG(), 8, 32)).r === 0, 'the mask was not cleared');
      } finally {
        ask.restore();
      }
    });
  });

  it('shows the chosen tool as a pressed button', async () => {
    const el = await paint();
    const [brush, eraser] = [...el.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')];
    expect(brush?.getAttribute('aria-pressed')).to.equal('true');
    eraser!.click();
    await el.updateComplete;
    expect(brush?.getAttribute('aria-pressed')).to.equal('false');
    expect(eraser?.getAttribute('aria-pressed')).to.equal('true');
    expect(eraser?.className).to.contain('focus-visible:focus-ring');
  });

  describe('inside an htmx form', () => {
    const real = XMLHttpRequest.prototype.send;
    let sent: FormData[];

    beforeEach(() => {
      sent = [];
      // The request is counted and not sent: a form that htmx sends twice (once without the mask) shows up here.
      XMLHttpRequest.prototype.send = (body?: Document | XMLHttpRequestBodyInit | null) => {
        sent.push(body as FormData);
      };
    });

    afterEach(() => {
      XMLHttpRequest.prototype.send = real;
    });

    it('sends the form once, with the mask, and never lets the first submit go without it', async () => {
      const form = await fixture<HTMLFormElement>(html`
        <form hx-post="/mask-paint-test" hx-sync="this:drop" hx-encoding="multipart/form-data" enctype="multipart/form-data" hx-swap="none">
          <mam-mask-paint src=${flat(32)}></mam-mask-paint>
          <input type="file" name="mask" hidden />
          <button type="submit">送る</button>
        </form>
      `);
      htmx.process(form);
      const el = form.querySelector('mam-mask-paint') as MamMaskPaint;
      await waitUntil(() => el.querySelector('canvas')?.width === 32, 'the picture was not read');
      el.querySelector('canvas')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
      let submits = 0;
      form.addEventListener('submit', () => (submits += 1));
      form.requestSubmit();
      await waitUntil(() => sent.length > 0, 'the form was not sent', { timeout: 2000 });
      expect(submits).to.equal(1);
      expect(sent).to.have.length(1);
      const mask = sent[0]!.get('mask') as File;
      expect(mask.type).to.equal('image/png');
      expect(mask.size).to.be.greaterThan(0);
    });
  });
});
