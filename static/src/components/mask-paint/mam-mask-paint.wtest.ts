import { expect, fixture, html, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import './mam-mask-paint';
import type { MamMaskPaint } from './mam-mask-paint';

async function paint(): Promise<MamMaskPaint> {
  const el = await fixture<MamMaskPaint>(html`<mam-mask-paint width="32" height="32"></mam-mask-paint>`);
  await el.updateComplete;
  return el;
}

async function pixel(blob: Blob, x: number, y: number): Promise<{ r: number; g: number; b: number }> {
  const bmp = await createImageBitmap(blob);
  const c = document.createElement('canvas');
  c.width = bmp.width;
  c.height = bmp.height;
  const g = c.getContext('2d')!;
  g.drawImage(bmp, 0, 0);
  const p = g.getImageData(x, y, 1, 1).data;
  return { r: p[0] ?? 0, g: p[1] ?? 0, b: p[2] ?? 0 };
}

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
    [...el.querySelectorAll('button')].find((b) => b.textContent === '全部消す')!.click();
    const mid = await pixel(await el.maskPNG(), 16, 16);
    expect(mid.r).to.equal(0);
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

  it('shows the chosen tool as a pressed button', async () => {
    const el = await paint();
    const [brush, eraser] = [...el.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')];
    expect(brush?.getAttribute('aria-pressed')).to.equal('true');
    eraser!.click();
    await el.updateComplete;
    expect(brush?.getAttribute('aria-pressed')).to.equal('false');
    expect(eraser?.getAttribute('aria-pressed')).to.equal('true');
    expect(eraser?.className).to.contain('focus-visible:ring-2');
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
          <mam-mask-paint width="32" height="32"></mam-mask-paint>
          <input type="file" name="mask" hidden />
          <button type="submit">送る</button>
        </form>
      `);
      htmx.process(form);
      const el = form.querySelector('mam-mask-paint') as MamMaskPaint;
      await el.updateComplete;
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
