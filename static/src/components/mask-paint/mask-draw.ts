// What is drawn on the two canvases of <mam-mask-paint>: the mask (black keeps, white redraws) and the view (the overlay of what is painted).

export type Point = { x: number; y: number };

/** Draws a round-capped line of `size` from `from` to `to` (a dab when they are the same) on `canvas`. */
export function strokeLine(
  canvas: HTMLCanvasElement,
  from: Point,
  to: Point,
  size: number,
  color: string,
  op: GlobalCompositeOperation,
): void {
  const g = canvas.getContext('2d');
  if (!g) return;
  g.save();
  g.globalCompositeOperation = op;
  g.fillStyle = color;
  g.strokeStyle = color;
  g.lineWidth = size;
  g.lineCap = 'round';
  g.beginPath();
  g.arc(from.x, from.y, size / 2, 0, Math.PI * 2);
  g.fill();
  g.beginPath();
  g.moveTo(from.x, from.y);
  g.lineTo(to.x, to.y);
  g.stroke();
  g.restore();
}

/** Paints the overlay of `view` in `color` where `mask` is white: the red channel of the mask becomes the alpha of the overlay. */
export function overlayWhite(view: CanvasRenderingContext2D, mask: CanvasRenderingContext2D, color: string): void {
  const { width: w, height: h } = mask.canvas;
  const white = mask.getImageData(0, 0, w, h);
  const alpha = new ImageData(w, h);
  for (let i = 0; i < white.data.length; i += 4) alpha.data[i + 3] = white.data[i] ?? 0;
  const layer = document.createElement('canvas');
  layer.width = w;
  layer.height = h;
  layer.getContext('2d')?.putImageData(alpha, 0, 0);
  view.save();
  view.fillStyle = color;
  view.fillRect(0, 0, w, h);
  view.globalCompositeOperation = 'destination-in';
  view.drawImage(layer, 0, 0);
  view.restore();
}

/** The mean relative luminance (0 black, 1 white; WCAG, in linear light) of `canvas`, read from a thumbnail so a big picture costs nothing; null when the picture cannot be read. */
export function meanLuminance(canvas: HTMLCanvasElement): number | null {
  const edge = 16;
  const thumb = document.createElement('canvas');
  thumb.width = edge;
  thumb.height = edge;
  const g = thumb.getContext('2d', { willReadFrequently: true });
  if (!g) return null;
  try {
    g.drawImage(canvas, 0, 0, edge, edge);
    const px = g.getImageData(0, 0, edge, edge).data;
    let sum = 0;
    for (let i = 0; i < px.length; i += 4) {
      sum += 0.2126 * toLinear(px[i] ?? 0) + 0.7152 * toLinear(px[i + 1] ?? 0) + 0.0722 * toLinear(px[i + 2] ?? 0);
    }
    return sum / (edge * edge);
  } catch {
    return null; // a picture of another origin taints the canvas: the pixels cannot be read
  }
}

function toLinear(channel: number): number {
  const c = channel / 255;
  return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

/**
 * Below this mean luminance the overlay is the pale token, from it the deep one: the two keep 3:1 on the picture at the same luminance (about
 * 0.11, a grey of 96), so the one that is less visible is never chosen (spec/components/color-rules.md §4.2).
 */
export const PALE_BELOW = 0.11;

/** The token of the overlay colour for a picture of this mean luminance (the deep one when it is not known). */
export function overlayToken(luminance: number | null): '--overlay-deep' | '--overlay-pale' {
  return luminance !== null && luminance < PALE_BELOW ? '--overlay-pale' : '--overlay-deep';
}
