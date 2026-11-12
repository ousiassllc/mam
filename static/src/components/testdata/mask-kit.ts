import { fixture, html, waitUntil } from '@open-wc/testing';
import '../mask-paint/mam-mask-paint';
import type { MamMaskPaint } from '../mask-paint/mam-mask-paint';

/** A flat picture of `size` × `size` as a PNG data URL: the painter takes its canvases' size from it. */
export function flat(size: number, colour = '#336699'): string {
  const c = document.createElement('canvas');
  c.width = size;
  c.height = size;
  const g = c.getContext('2d')!;
  g.fillStyle = colour;
  g.fillRect(0, 0, size, size);
  return c.toDataURL('image/png');
}

/** A painter over a picture of 32 × 32 (without a picture there is no canvas), ready to be painted on. */
export async function paint(): Promise<MamMaskPaint> {
  const el = await fixture<MamMaskPaint>(html`<mam-mask-paint src=${flat(32)}></mam-mask-paint>`);
  await waitUntil(() => el.querySelector('canvas')?.width === 32, 'the picture was not read');
  return el;
}

export async function pixel(blob: Blob, x: number, y: number): Promise<{ r: number; g: number; b: number }> {
  const bmp = await createImageBitmap(blob);
  const c = document.createElement('canvas');
  c.width = bmp.width;
  c.height = bmp.height;
  const g = c.getContext('2d')!;
  g.drawImage(bmp, 0, 0);
  const p = g.getImageData(x, y, 1, 1).data;
  return { r: p[0] ?? 0, g: p[1] ?? 0, b: p[2] ?? 0 };
}

export const clearButton = (el: Element) =>
  [...el.querySelectorAll('button')].find((b) => b.textContent === '全部消す')!;
