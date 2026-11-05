/**
 * The ripple of a press on a button (the Ousiass UI guide, M4): a circle grows from the point that was pressed until it covers the
 * button, and fades out, in --motion-slow with --ease-out.
 */

export interface Ripple {
  /** The circle's left and top, in px, from the button's top-left corner. */
  left: number;
  top: number;
  /** The circle's diameter: twice the distance to the farthest corner, so that it covers the whole button. */
  size: number;
}

/** The circle for a press at (`x`, `y`) inside a `width` × `height` button (both from the button's top-left corner). */
export function rippleAt(x: number, y: number, width: number, height: number): Ripple {
  const farthest = Math.hypot(Math.max(x, width - x), Math.max(y, height - y));
  const size = Math.ceil(farthest * 2);
  return { left: x - size / 2, top: y - size / 2, size };
}

/** A CSS time such as `280ms` or `0.28s` in milliseconds (0 when it is not one). */
export function milliseconds(time: string): number {
  const match = /^\s*(-?[\d.]+)(ms|s)\s*$/.exec(time);
  if (!match) return 0;
  const value = Number(match[1]);
  return match[2] === 's' ? value * 1000 : value;
}
