import { describe, expect, it } from 'vitest';
import { isFinished } from './shot';

describe('isFinished', () => {
  it('is true once the work is over or nothing is running', () => {
    expect(['done', 'failed', 'idle'].map((stage) => isFinished({ stage: stage as never }))).toEqual([true, true, true]);
  });
  it('is false while the Shot is queued or generating', () => {
    expect(['queued', 'generating', 'judging'].map((stage) => isFinished({ stage: stage as never }))).toEqual([false, false, false]);
  });
});
