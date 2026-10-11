import { describe, expect, it } from 'vitest';
import {
  clipStarts,
  isDirty,
  moveClip,
  removeClip,
  setCaption,
  setTransition,
  setTrim,
  timelineSeconds,
  toPayload,
  captionProblem,
  trimProblem,
  type EditClip,
  type EditLimits,
} from './edit-model';

const limits: EditLimits = { min_clip_seconds: 0.5, max_transition_seconds: 2, max_caption_runes: 10 };

function clip(shot: string, inPoint: number, outPoint: number, over: Partial<EditClip> = {}): EditClip {
  return {
    shot_id: shot,
    take_id: `t-${shot}`,
    label: shot,
    source_seconds: 6,
    in_point: inPoint,
    out_point: outPoint,
    start: 0,
    transition: { type: 'cut', seconds: 0 },
    ...over,
  };
}

const three = () => [clip('a', 0, 4), clip('b', 1, 3, { transition: { type: 'fade', seconds: 0.5 } }), clip('c', 0, 2)];

describe('timeline length', () => {
  it('overlaps the clips of a transition', () => {
    expect(clipStarts(three())).toEqual([0, 3.5, 5.5]);
    expect(timelineSeconds(three())).toBe(7.5);
  });

  it('caps a transition at half of the shorter clip', () => {
    const clips = [clip('a', 0, 4), clip('b', 0, 1, { transition: { type: 'dissolve', seconds: 2 } })];
    expect(timelineSeconds(clips)).toBe(4.5);
  });
});

describe('moveClip', () => {
  it('reorders the clips and recomputes the starts', () => {
    const moved = moveClip(three(), 2, 0);
    expect(moved.map((c) => c.shot_id)).toEqual(['c', 'a', 'b']);
    expect(moved.map((c) => c.start)).toEqual([0, 2, 5.5]);
  });

  it('ignores a move out of range', () => {
    const clips = three();
    expect(moveClip(clips, 0, 3)).toBe(clips);
    expect(moveClip(clips, 1, 1)).toBe(clips);
  });
});

describe('removeClip', () => {
  it('leaves a clip out but keeps at least one', () => {
    expect(removeClip(three(), 1).map((c) => c.shot_id)).toEqual(['a', 'c']);
    const one = [clip('a', 0, 2)];
    expect(removeClip(one, 0)).toBe(one);
  });
});

describe('setTrim', () => {
  it('changes the Cut Point', () => {
    const next = setTrim(three(), 0, 1, 3, limits);
    expect(next[0]).toMatchObject({ in_point: 1, out_point: 3 });
    expect(timelineSeconds(next)).toBe(5.5);
  });

  it.each([
    ['shorter than the minimum', 0, 0.2, '0.5 秒以上'],
    ['before the start', -1, 2, '0 秒以上'],
    ['past the end of the Take', 0, 7, 'Take の長さ'],
    ['not a number', Number.NaN, 2, '数値'],
  ])('refuses %s', (_name, inPoint, outPoint, message) => {
    const clips = three();
    expect(trimProblem(clips[0]!, inPoint, outPoint, limits)).toContain(message);
    expect(setTrim(clips, 0, inPoint, outPoint, limits)).toBe(clips);
  });
});

describe('setTransition', () => {
  it('sets the type and clamps the length', () => {
    const next = setTransition(three(), 2, 'dissolve', 9, limits);
    expect(next[2]!.transition).toEqual({ type: 'dissolve', seconds: 2 });
  });

  it('gives a hard cut no length and the first clip no transition', () => {
    const clips = three();
    expect(setTransition(clips, 1, 'cut', 1, limits)[1]!.transition).toEqual({ type: 'cut', seconds: 0 });
    expect(setTransition(clips, 0, 'fade', 1, limits)).toBe(clips);
  });
});

describe('save', () => {
  it('sends what the server needs and nothing derived', () => {
    const payload = toPayload(moveClip(three(), 1, 0));
    expect(Object.keys(payload.clips[0]!).sort()).toEqual([
      'caption',
      'in_point',
      'kind',
      'out_point',
      'shot_id',
      'take_id',
      'transition',
    ]);
    expect(payload.clips[0]!.transition).toEqual({ type: 'cut', seconds: 0 }); // the new first clip
  });

  it('is dirty only after a change that saves differently', () => {
    const saved = three();
    expect(isDirty(saved, three())).toBe(false);
    expect(isDirty(saved, moveClip(three(), 0, 1))).toBe(true);
  });
});

describe('caption and clip kind', () => {
  it('sets, trims and clears the caption of one clip only', () => {
    const set = setCaption(three(), 1, '  朝が来た ', limits);
    expect(set[1]!.caption).toBe('朝が来た');
    expect(set[0]!.caption).toBeUndefined();
    expect(setCaption(set, 1, '', limits)[1]!.caption).toBe('');
  });

  it('refuses a caption that is too long and keeps the clips as they are', () => {
    const clips = three();
    const text = 'あ'.repeat(11);
    expect(captionProblem(text, limits)).toContain('10 文字以内');
    expect(setCaption(clips, 0, text, limits)).toBe(clips);
    expect(captionProblem('あ'.repeat(10), limits)).toBe('');
  });

  it('keeps the kind and caption of a clip through a reorder and a save', () => {
    const clips = three();
    clips[2] = { ...clips[2]!, kind: 'reaction', caption: 'えっ？' };
    const payload = toPayload(moveClip(clips, 2, 0));
    expect(payload.clips[0]).toMatchObject({ shot_id: 'c', kind: 'reaction', caption: 'えっ？' });
    expect(payload.clips[1]).toMatchObject({ kind: 'main', caption: '' });
  });

  it('is dirty when only a caption changed', () => {
    const saved = three();
    expect(isDirty(saved, setCaption(saved, 0, 'こんにちは', limits))).toBe(true);
  });
});
