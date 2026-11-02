// The fill of the common slider (#955) without the DOM. slider.css paints the rail up to `--fill` (0..1), and the browser knows
// no such thing for the rail of a range input in WebKit: the fill is written from the value, here.

/** What a range input holds: its value and its bounds, as the attributes write them (the browser's defaults are 0 and 100). */
export interface RangeLike {
  value: string;
  min: string;
  max: string;
}

/** Where the value lies between the bounds, 0..1 (0 when the bounds leave no room, or are not numbers). */
export function sliderFraction({ value, min, max }: RangeLike): number {
  const lo = min === '' ? 0 : Number(min);
  const hi = max === '' ? 100 : Number(max);
  const v = Number(value);
  if (!Number.isFinite(lo) || !Number.isFinite(hi) || !Number.isFinite(v) || hi <= lo) return 0;
  return Math.min(1, Math.max(0, (v - lo) / (hi - lo)));
}

/** The inline style that carries the fraction to slider.css. */
export const sliderStyle = (range: RangeLike): string => `--fill:${sliderFraction(range)}`;
