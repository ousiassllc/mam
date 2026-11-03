/**
 * The size of the canvases of `<mam-mask-paint>` (Issue #999).
 *
 * The painter holds two canvases of the picture's size (the mask and the red view of it), 4 bytes a pixel each: an 8192 × 8192 picture is 256 MiB
 * a canvas, and a browser refuses a canvas of that size on some devices (Safari on iOS stops at 16 megapixels). The mask does not need the picture's
 * pixels, only its shape: the nodes of ComfyUI that read it stretch it to the picture (the graphs of `inpaint-*`, `SetLatentNoiseMask` and
 * `ImageCompositeMasked`). So a picture whose longer side is over MAX_EDGE is painted on canvases scaled down to it; a smaller one is painted at
 * its own size, and its mask is exactly the size of the picture.
 */

/** The longest side, in pixels, of the canvases (2048 × 2048 is 16 MiB a canvas). */
export const MAX_EDGE = 2048;

/** The size of the canvases for a picture of `width` × `height`: the picture's own size, or scaled down (the shape kept) to MAX_EDGE on the longer side. */
export function canvasSize(width: number, height: number, maxEdge = MAX_EDGE): { width: number; height: number } {
  const w = Math.max(1, Math.round(width));
  const h = Math.max(1, Math.round(height));
  const longer = Math.max(w, h);
  if (longer <= maxEdge) return { width: w, height: h };
  const scale = maxEdge / longer;
  return { width: Math.max(1, Math.round(w * scale)), height: Math.max(1, Math.round(h * scale)) };
}
