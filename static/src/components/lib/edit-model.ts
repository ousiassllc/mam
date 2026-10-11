/**
 * The Edit Timeline of mam-timeline-editor as pure functions (F-18: Shot Selection, Cut Point,
 * Timing, Transition, and the Advanced items Caption, Reaction Shot and Insert Shot). The server has the last word (domain.EditTimeline.Normalize); these functions
 * keep the editor from offering what it would refuse.
 */

export type TransitionType = 'cut' | 'fade' | 'fade_black' | 'dissolve';

export interface EditTransition {
  type: TransitionType;
  seconds: number;
}

/** What a clip is: the story, or a cutaway added in editing (Insert Shot / Reaction Shot). */
export type ClipKind = 'main' | 'insert' | 'reaction';

export const CLIP_KIND_LABEL: Record<ClipKind, string> = {
  main: '',
  insert: 'インサート',
  reaction: 'リアクション',
};

/** A clip of GET /api/v1/projects/:id/edit-timeline. */
export interface EditClip {
  shot_id: string;
  take_id: string;
  label: string;
  kind?: ClipKind;
  /** The text drawn over the clip (Caption). */
  caption?: string;
  objective?: string;
  source_seconds?: number;
  in_point: number;
  out_point: number;
  start: number;
  transition: EditTransition;
  video_url?: string;
  thumbnail_url?: string;
}

export interface EditLimits {
  min_clip_seconds: number;
  max_transition_seconds: number;
  max_clips: number;
  max_caption_runes: number;
}

export const TRANSITION_LABEL: Record<TransitionType, string> = {
  cut: 'カット',
  fade: 'フェード',
  fade_black: '暗転',
  dissolve: 'ディゾルブ',
};

const round = (value: number) => Math.round(value * 1000) / 1000;

/** Length of a clip on the timeline (Timing). */
export function clipLength(clip: Pick<EditClip, 'in_point' | 'out_point'>): number {
  return round(clip.out_point - clip.in_point);
}

/** A transition is at most half of the shorter of the two clips it joins. */
export function effectiveOverlap(clips: EditClip[], index: number): number {
  if (index <= 0 || index >= clips.length) return 0;
  const t = clips[index]!.transition;
  if (t.type === 'cut') return 0;
  const shorter = Math.min(clipLength(clips[index - 1]!), clipLength(clips[index]!));
  return Math.min(t.seconds, shorter / 2);
}

/** Second each clip begins at in the finished video. */
export function clipStarts(clips: EditClip[]): number[] {
  const starts: number[] = [];
  let at = 0;
  clips.forEach((clip, i) => {
    at = i === 0 ? 0 : at - effectiveOverlap(clips, i);
    starts.push(round(at));
    at += clipLength(clip);
  });
  return starts;
}

/** Total length of the finished video. */
export function timelineSeconds(clips: EditClip[]): number {
  if (clips.length === 0) return 0;
  const last = clips.length - 1;
  return round(clipStarts(clips)[last]! + clipLength(clips[last]!));
}

/** Recomputes `start` of every clip. */
export function withStarts(clips: EditClip[]): EditClip[] {
  const starts = clipStarts(clips);
  return clips.map((c, i) => ({ ...c, start: starts[i]! }));
}

/** Moves the clip at `from` to `to` (Shot Selection: the order). The first clip never has a transition. */
export function moveClip(clips: EditClip[], from: number, to: number): EditClip[] {
  if (from === to || from < 0 || to < 0 || from >= clips.length || to >= clips.length) return clips;
  const next = clips.slice();
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved!);
  return withStarts(next);
}

/** Leaves a clip out of the cut (Shot Selection). The last clip cannot be removed. */
export function removeClip(clips: EditClip[], index: number): EditClip[] {
  if (clips.length <= 1 || index < 0 || index >= clips.length) return clips;
  return withStarts(clips.filter((_, i) => i !== index));
}

/** Why the In / Out of a clip is not acceptable ('' when it is). */
export function trimProblem(clip: EditClip, inPoint: number, outPoint: number, limits: EditLimits): string {
  if (!Number.isFinite(inPoint) || !Number.isFinite(outPoint)) return '数値で指定してください';
  if (inPoint < 0) return '開始は 0 秒以上にしてください';
  if (outPoint - inPoint < limits.min_clip_seconds - 1e-9)
    return `${limits.min_clip_seconds} 秒以上の区間にしてください`;
  const source = clip.source_seconds ?? 0;
  if (source > 0 && outPoint > source + 1e-6) return `Take の長さ（${source.toFixed(1)} 秒）を超えています`;
  return '';
}

/** Sets the Cut Point (In / Out) of a clip. Returns `clips` itself when the change is not acceptable. */
export function setTrim(
  clips: EditClip[],
  index: number,
  inPoint: number,
  outPoint: number,
  limits: EditLimits,
): EditClip[] {
  const clip = clips[index];
  if (!clip || trimProblem(clip, inPoint, outPoint, limits) !== '') return clips;
  const next = clips.slice();
  next[index] = { ...clip, in_point: round(inPoint), out_point: round(outPoint) };
  return withStarts(next);
}

/** Sets the transition into a clip. The first clip has none; a hard cut has no length. */
export function setTransition(
  clips: EditClip[],
  index: number,
  type: TransitionType,
  seconds: number,
  limits: EditLimits,
): EditClip[] {
  const clip = clips[index];
  if (!clip || index === 0) return clips;
  const length = type === 'cut' ? 0 : round(Math.min(Math.max(seconds, 0.1), limits.max_transition_seconds));
  const next = clips.slice();
  next[index] = { ...clip, transition: { type, seconds: length } };
  return withStarts(next);
}

/** Why a caption is not acceptable ('' when it is). Line breaks are the server's to tidy; only the length is checked. */
export function captionProblem(text: string, limits: EditLimits): string {
  const length = [...text.trim()].length;
  return length > limits.max_caption_runes
    ? `字幕は ${limits.max_caption_runes} 文字以内にしてください（現在 ${length} 文字）`
    : '';
}

/** Sets the caption of a clip ('' removes it). Returns `clips` itself when the text is too long. */
export function setCaption(clips: EditClip[], index: number, text: string, limits: EditLimits): EditClip[] {
  const clip = clips[index];
  if (!clip || captionProblem(text, limits) !== '') return clips;
  const caption = text.trim();
  if ((clip.caption ?? '') === caption) return clips;
  const next = clips.slice();
  next[index] = { ...clip, caption };
  return next;
}

/** The body of PUT /edit-timelines/:id. */
export function toPayload(clips: EditClip[]): {
  clips: Array<
    Pick<EditClip, 'shot_id' | 'take_id' | 'in_point' | 'out_point' | 'transition'> & {
      kind: ClipKind;
      caption: string;
    }
  >;
} {
  return {
    clips: clips.map((c, i) => ({
      shot_id: c.shot_id,
      take_id: c.take_id,
      kind: c.kind ?? 'main',
      caption: (c.caption ?? '').trim(),
      in_point: c.in_point,
      out_point: c.out_point,
      transition: i === 0 ? { type: 'cut', seconds: 0 } : c.transition,
    })),
  };
}

/** True when the two lists would save differently. */
export function isDirty(saved: EditClip[], current: EditClip[]): boolean {
  return JSON.stringify(toPayload(saved)) !== JSON.stringify(toPayload(current));
}
