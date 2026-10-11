import { describe, expect, it } from 'vitest';
import { createRefreshGuard, describeProgress, isFinished, planCardRefresh, progressFraction } from './shot';

describe('isFinished', () => {
  it('is true once the work is over or nothing is running', () => {
    expect(['done', 'failed', 'idle'].map((stage) => isFinished({ stage: stage as never }))).toEqual([
      true,
      true,
      true,
    ]);
  });
  it('is false while the Shot is queued or generating', () => {
    expect(['queued', 'generating', 'judging'].map((stage) => isFinished({ stage: stage as never }))).toEqual([
      false,
      false,
      false,
    ]);
  });
});

describe('describeProgress', () => {
  it('tells what the placeholder is waiting for', () => {
    expect(describeProgress(null)).toBe('動画はまだありません');
    expect(describeProgress({ stage: 'idle' })).toBe('動画はまだありません');
    expect(describeProgress({ stage: 'queued' })).toBe('生成の順番を待っています');
    expect(describeProgress({ stage: 'queued', action: 'judge' })).toBe('Judge の順番を待っています');
    expect(describeProgress({ stage: 'generating', takes_done: 1, takes_total: 3 })).toBe('生成中 1 / 3 Take');
    expect(describeProgress({ stage: 'queued', action: 'candidates' })).toBe('生成の順番を待っています');
    expect(describeProgress({ stage: 'generating', action: 'candidates', takes_done: 1, takes_total: 3 })).toBe(
      '生成中 1 / 3 Take',
    );
    expect(describeProgress({ stage: 'generating' })).toBe('生成中');
    expect(describeProgress({ stage: 'queued', action: 'repair' })).toBe('修正の順番を待っています');
    expect(describeProgress({ stage: 'repairing', action: 'repair' })).toBe('修正中');
    expect(describeProgress({ stage: 'failed', message: '予算超過' })).toBe('失敗しました: 予算超過');
  });
});

describe('progressFraction', () => {
  it('measures generation by Takes and is null when there is no measure', () => {
    expect(progressFraction({ stage: 'generating', takes_done: 1, takes_total: 4 })).toBe(0.25);
    expect(progressFraction({ stage: 'generating', takes_done: 9, takes_total: 4 })).toBe(1);
    expect(progressFraction({ stage: 'done' })).toBe(1);
    expect(progressFraction({ stage: 'queued' })).toBeNull();
    expect(progressFraction(null)).toBeNull();
  });
});

describe('createRefreshGuard', () => {
  it('lets an idle-triggered refresh of a card URL through once per window so an idle/re-render cycle cannot loop', () => {
    let t = 1000;
    const mayRefresh = createRefreshGuard(3000, () => t);
    expect(mayRefresh('/shots/a', 'idle')).toBe(true);
    t += 100;
    expect(mayRefresh('/shots/a', 'idle')).toBe(false);
    expect(mayRefresh('/shots/b', 'idle')).toBe(true);
    t += 3000;
    expect(mayRefresh('/shots/a', 'idle')).toBe(true);
  });

  it('never drops the refresh of work that ended, even right after another refresh (a fast Judge / Repair)', () => {
    let t = 1000;
    const mayRefresh = createRefreshGuard(3000, () => t);
    expect(mayRefresh('/shots/a', 'done')).toBe(true);
    t += 1000;
    expect(mayRefresh('/shots/a', 'done')).toBe(true);
    t += 10;
    expect(mayRefresh('/shots/a', 'failed')).toBe(true);
    t += 10;
    expect(mayRefresh('/shots/a', 'idle')).toBe(false);
  });
});

describe('planCardRefresh', () => {
  const card = { id: 'shot-b', refreshUrl: '/projects/p/scenes/s?shot=b' };

  it('refreshes the whole Shot view when the selected Shot finished', () => {
    expect(planCardRefresh({ ...card, selected: true }, '/projects/p/scenes/s?shot=b')).toEqual({
      url: '/projects/p/scenes/s?shot=b',
      target: '#scene-shot-view',
      swap: 'outerHTML',
    });
  });

  it('refreshes only the finished card, from the selected Shot page, when another Shot is viewed', () => {
    expect(planCardRefresh({ ...card, selected: false }, '/projects/p/scenes/s?shot=a')).toEqual({
      url: '/projects/p/scenes/s?shot=a',
      target: '#shot-b',
      swap: 'outerHTML',
      select: '#shot-b',
    });
  });

  it('asks for the Scene page without ?shot= when no Shot is selected, so the card is not rendered as selected', () => {
    expect(planCardRefresh({ ...card, selected: false }, null).url).toBe('/projects/p/scenes/s');
  });
});
