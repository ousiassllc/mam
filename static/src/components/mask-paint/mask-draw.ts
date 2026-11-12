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
