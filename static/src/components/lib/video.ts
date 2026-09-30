/**
 * Frame-accurate seeking and timecode conversion for mam-video-player. A <video> element does not
 * tell its frame rate, so every function takes it as a parameter. Pure functions only; the
 * element is touched by seekToFrame alone.
 */

/** An Issue of GET /api/v1/takes/:id/judge-result (the parts the player draws). */
export interface JudgeIssue {
  type: string;
  severity: 'high' | 'medium' | 'low';
  /** "開始-終了s" in seconds, for example "2.1-2.8s". */
  time_range?: string;
  /** [first frame, last frame] of the source video, both included. */
  frames?: [number, number];
  description?: string;
}

/** A stretch of the video in seconds. */
export interface TimeRange {
  start: number;
  end: number;
}

/** Rates a video is usually made in; a measured rate snaps to the nearest of them. */
const STANDARD_RATES = [23.976, 24, 25, 29.97, 30, 48, 50, 59.94, 60];

export const DEFAULT_FPS = 30;

/** Time (seconds) in the middle of frame `frame`: seeking to the start of a frame can land on the previous one. */
export function frameToTime(frame: number, fps: number): number {
  return (Math.max(frame, 0) + 0.5) / fps;
}

/** Number of the frame shown at `time` seconds (0-based). */
export function timeToFrame(time: number, fps: number): number {
  return Math.max(Math.floor(time * fps + 1e-6), 0);
}

/** Time (seconds) of the first instant of the frame at `time`: the value to show and to store. */
export function snapToFrame(time: number, fps: number): number {
  return timeToFrame(time, fps) / fps;
}

/** Number of frames in a video of `duration` seconds. */
export function frameCount(duration: number, fps: number): number {
  return Math.max(Math.round(duration * fps), 0);
}

/** "MM:SS:FF" (with hours when there are any): the timecode of the frame at `time`. */
export function formatTimecode(time: number, fps: number): string {
  const total = timeToFrame(Math.max(time, 0), fps);
  const rate = Math.max(Math.round(fps), 1);
  const frames = total % rate;
  const seconds = Math.floor(total / rate);
  const two = (n: number) => String(n).padStart(2, '0');
  const hours = Math.floor(seconds / 3600);
  const body = `${two(Math.floor((seconds % 3600) / 60))}:${two(seconds % 60)}:${two(frames)}`;
  return hours > 0 ? `${two(hours)}:${body}` : body;
}

/** The frame number `delta` frames away from the one at `time`, kept inside the video. */
export function stepFrame(time: number, delta: number, fps: number, duration: number): number {
  const last = Math.max(frameCount(duration, fps) - 1, 0);
  return Math.min(Math.max(timeToFrame(time, fps) + delta, 0), last);
}

/** Reads "2.1-2.8s" (seconds). Anything else is null. */
export function parseTimeRange(text: string | undefined): TimeRange | null {
  const m = /^\s*(\d+(?:\.\d+)?)\s*-\s*(\d+(?:\.\d+)?)\s*s?\s*$/.exec(text ?? '');
  if (!m) return null;
  const start = Number(m[1]);
  const end = Number(m[2]);
  return end >= start ? { start, end } : null;
}

/**
 * Where an Issue lies in the video. The frame range is the exact one, so it wins over the time
 * range when the frame rate is known (`fps` > 0); the last frame is included, so the range ends
 * where that frame ends. Without a known rate only the time range can be trusted.
 */
export function issueRange(issue: Pick<JudgeIssue, 'frames' | 'time_range'>, fps: number): TimeRange | null {
  const frames = issue.frames;
  if (fps > 0 && frames && frames.length === 2 && frames[1] >= frames[0]) {
    return { start: frames[0] / fps, end: (frames[1] + 1) / fps };
  }
  return parseTimeRange(issue.time_range);
}

/** Frame rate that a series of presentation times (seconds) implies, snapped to a standard rate. 0 when unknown. */
export function estimateFps(mediaTimes: number[]): number {
  const times = [...new Set(mediaTimes)].sort((a, b) => a - b);
  let step = Infinity;
  for (let i = 1; i < times.length; i += 1) {
    const d = (times[i] as number) - (times[i - 1] as number);
    if (d > 1e-4) step = Math.min(step, d);
  }
  if (!Number.isFinite(step)) return 0;
  const rate = 1 / step;
  const nearest = STANDARD_RATES.reduce((best, r) => (Math.abs(r - rate) < Math.abs(best - rate) ? r : best));
  return Math.abs(nearest - rate) / nearest < 0.02 ? nearest : Math.round(rate * 1000) / 1000;
}

/** Puts the element on frame `frame` (paused, so that the frame stays on screen). Returns the time it seeked to. */
export function seekToFrame(video: Pick<HTMLVideoElement, 'currentTime' | 'duration' | 'pause'>, frame: number, fps: number): number {
  video.pause();
  let time = frameToTime(frame, fps);
  if (Number.isFinite(video.duration)) time = Math.min(time, Math.max(video.duration - 0.5 / fps, 0));
  video.currentTime = time;
  return time;
}
