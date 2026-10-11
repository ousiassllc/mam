/**
 * Data model shared by the timeline components (mam-take-comparator today, mam-timeline-editor
 * later): a stretch of a clip, and the operations on a list of them. Pure functions only.
 */
import { snapToFrame } from './video';

/** A part of a Take: `start` to `end` seconds. */
export interface Segment {
  take_id: string;
  start: number;
  end: number;
}

/** The shortest segment a composition accepts: a cross-fade needs something to fade (same as the server). */
export const MIN_SEGMENT_SECONDS = 0.3;
/** The most segments one composition accepts (same as the server). */
export const MAX_SEGMENTS = 8;

/** A clip a segment can be taken from. */
export interface Clip {
  id: string;
  /** "A", "B", ... */
  label: string;
  duration: number;
  fps: number;
}

/**
 * The segment a drag from `fromX` to `toX` (pixels within a lane `width` wide showing `duration`
 * seconds) selects, snapped to frames and kept inside the clip. The drag may go in either direction.
 */
export function dragToSegment(clip: Clip, fromX: number, toX: number, width: number): Segment {
  const at = (x: number) => Math.min(Math.max(x / width, 0), 1) * clip.duration;
  const [a, b] = fromX <= toX ? [at(fromX), at(toX)] : [at(toX), at(fromX)];
  const start = snapToFrame(a, clip.fps);
  const end = Math.min(Math.max(snapToFrame(b, clip.fps), start), clip.duration);
  return { take_id: clip.id, start, end };
}

/** Why a segment cannot be part of a composition, or '' when it can. */
export function segmentProblem(segment: Segment, clip: Clip | undefined): string {
  if (!clip) return 'Take が見つかりません';
  if (segment.end - segment.start < MIN_SEGMENT_SECONDS - 1e-9)
    return `${MIN_SEGMENT_SECONDS} 秒以上の区間を選んでください`;
  if (clip.duration > 0 && segment.end > clip.duration + 0.001) return 'Take の長さを超えています';
  return '';
}

/** `segments` with `segment` appended when it is acceptable, else unchanged. */
export function addSegment(segments: Segment[], segment: Segment, clips: Clip[]): Segment[] {
  if (segments.length >= MAX_SEGMENTS) return segments;
  if (
    segmentProblem(
      segment,
      clips.find((c) => c.id === segment.take_id),
    )
  )
    return segments;
  return [...segments, segment];
}

/** `segments` without the one at `index`. */
export function removeSegment(segments: Segment[], index: number): Segment[] {
  return segments.filter((_, i) => i !== index);
}

/** Length of the composition in seconds (before the cross-fades at the joints). */
export function totalSeconds(segments: Segment[]): number {
  return segments.reduce((sum, s) => sum + (s.end - s.start), 0);
}

/** "Take A の 0.0-2.0秒 + Take B の 2.0-4.0秒": the words the server also understands. */
export function describeSegments(segments: Segment[], clips: Clip[]): string {
  return segments
    .map((s) => {
      const label = clips.find((c) => c.id === s.take_id)?.label ?? '?';
      return `Take ${label} の ${s.start.toFixed(1)}-${s.end.toFixed(1)}秒`;
    })
    .join(' + ');
}
