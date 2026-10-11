import { describe, expect, it } from 'vitest';
import {
  estimateFps,
  formatTimecode,
  frameToTime,
  issueRange,
  parseTimeRange,
  seekToFrame,
  snapToFrame,
  stepFrame,
  timeToFrame,
} from './video';

describe('frames and time', () => {
  it('seeks into the middle of a frame and reads the same frame back', () => {
    for (const fps of [24, 25, 29.97, 30, 60]) {
      for (const frame of [0, 1, 37, 119]) {
        expect(timeToFrame(frameToTime(frame, fps), fps)).toBe(frame);
      }
    }
  });

  it('snaps a time to the start of its frame', () => {
    expect(snapToFrame(1.03, 25)).toBeCloseTo(1.0);
    expect(snapToFrame(1.05, 25)).toBeCloseTo(1.04);
  });

  it('formats MM:SS:FF and adds hours only when needed', () => {
    expect(formatTimecode(0, 24)).toBe('00:00:00');
    expect(formatTimecode(2.5, 24)).toBe('00:02:12');
    expect(formatTimecode(61 + 1 / 24, 24)).toBe('01:01:01');
    expect(formatTimecode(3600, 30)).toBe('01:00:00:00');
  });

  it('steps by frames and stays inside the video', () => {
    expect(stepFrame(1.0, 1, 25, 4)).toBe(26);
    expect(stepFrame(1.0, -10, 25, 4)).toBe(15);
    expect(stepFrame(0.02, -1, 25, 4)).toBe(0);
    expect(stepFrame(3.99, 5, 25, 4)).toBe(99);
  });

  it('pauses and puts the element on the requested frame, not past the end', () => {
    const video = {
      currentTime: 0,
      duration: 4,
      paused: false,
      pause() {
        this.paused = true;
      },
    };
    expect(seekToFrame(video, 25, 25)).toBeCloseTo(1.02);
    expect(video.currentTime).toBeCloseTo(1.02);
    expect(video.paused).toBe(true);
    seekToFrame(video, 500, 25);
    expect(video.currentTime).toBeLessThan(4);
  });
});

describe('Issue ranges', () => {
  it('reads seconds ranges and refuses anything else', () => {
    expect(parseTimeRange('2.1-2.8s')).toEqual({ start: 2.1, end: 2.8 });
    expect(parseTimeRange(' 0 - 3 s ')).toEqual({ start: 0, end: 3 });
    expect(parseTimeRange('2.8-2.1s')).toBeNull();
    expect(parseTimeRange('around 3s')).toBeNull();
    expect(parseTimeRange(undefined)).toBeNull();
  });

  it('prefers the frame range and includes its last frame', () => {
    expect(issueRange({ frames: [63, 84], time_range: '2.1-2.8s' }, 30)).toEqual({ start: 2.1, end: 85 / 30 });
    expect(issueRange({ time_range: '3.0-3.9s' }, 30)).toEqual({ start: 3, end: 3.9 });
    expect(issueRange({}, 30)).toBeNull();
  });

  it('trusts only the time range while the frame rate is unknown', () => {
    expect(issueRange({ frames: [63, 84], time_range: '2.1-2.8s' }, 0)).toEqual({ start: 2.1, end: 2.8 });
    expect(issueRange({ frames: [63, 84] }, 0)).toBeNull();
  });
});

describe('estimateFps', () => {
  it('snaps the frame spacing to a standard rate', () => {
    expect(estimateFps([0, 0.04, 0.08, 0.12])).toBe(25);
    expect(estimateFps([0, 1 / 29.97, 2 / 29.97])).toBe(29.97);
    expect(estimateFps([0.1, 0.1, 0.2, 0.3])).toBe(10);
  });

  it('is 0 when there is nothing to compare', () => {
    expect(estimateFps([])).toBe(0);
    expect(estimateFps([1, 1])).toBe(0);
  });
});
