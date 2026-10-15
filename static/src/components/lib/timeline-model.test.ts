import { describe, expect, it } from 'vitest';
import {
  addSegment,
  describeSegments,
  dragToSegment,
  removeSegment,
  segmentProblem,
  totalSeconds,
  type Clip,
  type SegmentLimits,
} from './timeline-model';

const a: Clip = { id: 'a', label: 'A', duration: 4, fps: 25 };
const b: Clip = { id: 'b', label: 'B', duration: 4, fps: 25 };
const limits: SegmentLimits = { max_segments: 3, min_segment_seconds: 0.5 };

describe('dragToSegment', () => {
  it('maps a drag to frame-snapped seconds, in either direction', () => {
    expect(dragToSegment(a, 0, 200, 400)).toEqual({ take_id: 'a', start: 0, end: 2 });
    expect(dragToSegment(a, 200, 0, 400)).toEqual({ take_id: 'a', start: 0, end: 2 });
    const s = dragToSegment(a, 51, 149, 400);
    expect(s.start).toBeCloseTo(0.48);
    expect(s.end).toBeCloseTo(1.48);
  });

  it('keeps the drag inside the lane', () => {
    expect(dragToSegment(a, -50, 900, 400)).toEqual({ take_id: 'a', start: 0, end: 4 });
  });
});

describe('segments', () => {
  it('refuses segments that are too short or past the clip', () => {
    expect(segmentProblem({ take_id: 'a', start: 1, end: 1.1 }, a, limits)).not.toBe('');
    expect(segmentProblem({ take_id: 'a', start: 1, end: 4.5 }, a, limits)).not.toBe('');
    expect(segmentProblem({ take_id: 'zz', start: 0, end: 2 }, undefined, limits)).not.toBe('');
    expect(segmentProblem({ take_id: 'a', start: 0, end: 2 }, a, limits)).toBe('');
    expect(segmentProblem({ take_id: 'a', start: 1, end: 1.4 }, a, limits)).toContain('0.5 秒'); // the server's limit, not 0.3
  });

  it('adds acceptable segments in order, removes by index and limits the count', () => {
    let list = addSegment([], { take_id: 'a', start: 0, end: 2 }, [a, b], limits);
    list = addSegment(list, { take_id: 'b', start: 2, end: 4 }, [a, b], limits);
    expect(list.map((s) => s.take_id)).toEqual(['a', 'b']);
    expect(addSegment(list, { take_id: 'b', start: 1, end: 1.1 }, [a, b], limits)).toBe(list);
    expect(removeSegment(list, 0).map((s) => s.take_id)).toEqual(['b']);

    let full = list;
    for (let i = 0; i < limits.max_segments + 2; i += 1)
      full = addSegment(full, { take_id: 'a', start: 0, end: 1 }, [a, b], limits);
    expect(full).toHaveLength(limits.max_segments);
  });

  it('describes a composition in the words the server reads and sums its length', () => {
    const list = [
      { take_id: 'a', start: 0, end: 2 },
      { take_id: 'b', start: 2, end: 4 },
    ];
    expect(describeSegments(list, [a, b])).toBe('Take A の 0.0-2.0秒 + Take B の 2.0-4.0秒');
    expect(totalSeconds(list)).toBe(4);
  });
});
