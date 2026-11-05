import { describe, expect, it } from 'vitest';
import { milliseconds, rippleAt } from './ripple-model';

describe('rippleAt', () => {
  it('is centred on the press and reaches the farthest corner', () => {
    // A 100 × 40 button pressed at (10, 10): the farthest corner is (100, 40), 90 across and 30 down.
    const r = rippleAt(10, 10, 100, 40);

    expect(r.size).toBe(Math.ceil(Math.hypot(90, 30) * 2));
    expect(r.left).toBe(10 - r.size / 2);
    expect(r.top).toBe(10 - r.size / 2);
  });

  it('covers the button for a press in the middle too', () => {
    const r = rippleAt(50, 20, 100, 40);

    expect(r.size).toBe(Math.ceil(Math.hypot(50, 20) * 2));
  });
});

describe('milliseconds', () => {
  it('reads ms and s', () => {
    expect(milliseconds('280ms')).toBe(280);
    expect(milliseconds(' 0.28s ')).toBeCloseTo(280);
  });

  it('is 0 for what is not a time', () => {
    expect(milliseconds('')).toBe(0);
    expect(milliseconds('fast')).toBe(0);
  });
});
