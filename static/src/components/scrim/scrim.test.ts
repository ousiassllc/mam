import { describe, expect, it } from 'vitest';
import { isOnScrim } from './scrim';

const box = { left: 100, right: 300, top: 50, bottom: 250 };

describe('isOnScrim', () => {
  it("is false inside the box and on its edge (the dialog's own padding)", () => {
    expect(isOnScrim(box, 150, 100)).toBe(false);
    expect(isOnScrim(box, 100, 50)).toBe(false);
    expect(isOnScrim(box, 300, 250)).toBe(false);
  });

  it('is true outside it on any side', () => {
    expect(isOnScrim(box, 99, 100)).toBe(true);
    expect(isOnScrim(box, 301, 100)).toBe(true);
    expect(isOnScrim(box, 150, 49)).toBe(true);
    expect(isOnScrim(box, 150, 251)).toBe(true);
  });
});
