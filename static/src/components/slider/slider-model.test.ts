import { describe, expect, it } from 'vitest';
import { sliderFraction, sliderStyle } from './slider-model';

const range = (value: string, min: string, max: string) => ({ value, min, max });

describe('the fill of the common slider', () => {
  it('is where the value lies between the bounds', () => {
    expect(sliderFraction(range('0.7', '0', '1'))).toBeCloseTo(0.7);
    expect(sliderFraction(range('80', '50', '100'))).toBeCloseTo(0.6);
    expect(sliderFraction(range('50', '50', '100'))).toBe(0);
    expect(sliderFraction(range('100', '50', '100'))).toBe(1);
  });

  it('uses the browser defaults (0 and 100) for bounds the input does not write', () => {
    expect(sliderFraction(range('25', '', ''))).toBeCloseTo(0.25);
  });

  it('stays inside 0..1 and falls back to empty for bounds or a value that make no sense', () => {
    expect(sliderFraction(range('2', '0', '1'))).toBe(1);
    expect(sliderFraction(range('-1', '0', '1'))).toBe(0);
    expect(sliderFraction(range('0', '0', '0')), 'a disabled time bar of an unknown video').toBe(0);
    expect(sliderFraction(range('1', '1', '0'))).toBe(0);
    expect(sliderFraction(range('x', '0', '1'))).toBe(0);
  });

  it('is written as the custom property that slider.css reads', () => {
    expect(sliderStyle(range('0.5', '0', '1'))).toBe('--fill:0.5');
  });
});
