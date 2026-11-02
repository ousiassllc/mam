import { expect, fixture, html } from '@open-wc/testing';
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
    expect(el.querySelector('[role="radio"]')?.getAttribute('aria-checked')).to.equal('true');
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'e', bubbles: true }));
    await el.updateComplete;
    expect(el.querySelectorAll('[role="radio"]')[1]?.getAttribute('aria-checked')).to.equal('true');
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

  it('puts the PNG into the mask file input when the form is submitted', async () => {
    const form = await fixture<HTMLFormElement>(html`
      <form>
        <mam-mask-paint width="32" height="32"></mam-mask-paint>
        <input type="file" name="mask" />
        <button type="submit">送る</button>
      </form>
    `);
    const el = form.querySelector('mam-mask-paint') as MamMaskPaint;
    await el.updateComplete;
    el.querySelector('canvas')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    let submits = 0;
    form.addEventListener('submit', (e) => {
      submits += 1;
      e.preventDefault();
    });
    form.requestSubmit();
    await el.maskPNG();
    await new Promise((r) => setTimeout(r, 0));
    const file = form.querySelector<HTMLInputElement>('input[name="mask"]')!.files?.[0];
    expect(submits).to.be.greaterThan(0);
    expect(file?.type).to.equal('image/png');
    expect(file?.size).to.be.greaterThan(0);
  });
});
