import { describe, expect, it } from 'vitest';
import { imageProblem, showsLimits, targetText } from './match-model';

describe('the rules of the prompt match form', () => {
  const max = 20 << 20;

  it('takes a PNG, JPEG or WebP of at least one byte up to the limit', () => {
    for (const type of ['image/png', 'image/jpeg', 'image/webp'])
      expect(imageProblem({ type, size: 10 }, max)).toBe('');
    expect(imageProblem({ type: 'image/png', size: max }, max)).toBe('');
  });

  it('says why another file is refused', () => {
    expect(imageProblem({ type: 'image/gif', size: 10 }, max)).toContain('PNG・JPEG・WebP');
    expect(imageProblem({ type: 'application/pdf', size: 10 }, max)).toContain('PNG・JPEG・WebP');
    expect(imageProblem({ type: 'image/png', size: 0 }, max)).toContain('空');
    expect(imageProblem({ type: 'image/png', size: max + 1 }, max)).toContain('20 MiB');
  });

  it('shows the target and the limit of rounds only for the way that repeats on its own', () => {
    expect(showsLimits('auto')).toBe(true);
    expect(showsLimits('once')).toBe(false);
    expect(showsLimits('adjust')).toBe(false);
  });

  it('writes the target with a percent sign', () => {
    expect(targetText('80')).toBe('80%');
    expect(targetText(95)).toBe('95%');
  });
});
