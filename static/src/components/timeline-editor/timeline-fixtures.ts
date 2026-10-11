import { fixture, html, waitUntil } from '@open-wc/testing';
import './mam-timeline-editor';
import type { EditClip } from '../lib/edit-model';
import type { MamTimelineEditor } from './mam-timeline-editor';
import type { EditTimelineBody } from './timeline-api';

export const API = '/api/v1/projects/p1/edit-timeline';

export function clip(shot: string, inPoint: number, outPoint: number, over: Partial<EditClip> = {}): EditClip {
  return {
    shot_id: shot,
    take_id: `t-${shot}`,
    label: `Scene 1 · ${shot}`,
    objective: `objective of ${shot}`,
    source_seconds: 6,
    in_point: inPoint,
    out_point: outPoint,
    start: 0,
    transition: { type: 'cut', seconds: 0 },
    video_url: `/takes/t-${shot}/video`,
    thumbnail_url: `/takes/t-${shot}/thumbnail`,
    ...over,
  };
}

export function timeline(id: string, clips: EditClip[], over: Partial<EditTimelineBody> = {}): EditTimelineBody {
  return {
    id,
    version_number: 1,
    duration_seconds: 0,
    clips,
    save_url: `/edit-timelines/${id}`,
    preview_url: `/edit-timelines/${id}/preview`,
    transitions: ['cut', 'fade', 'fade_black', 'dissolve'],
    limits: { min_clip_seconds: 0.5, max_transition_seconds: 2, max_caption_runes: 10 },
    readonly: false,
    ...over,
  };
}

export const three = () => [clip('a', 0, 4), clip('b', 0, 3), clip('c', 0, 2)];

export async function editor(): Promise<MamTimelineEditor> {
  const el = await fixture<MamTimelineEditor>(html`<mam-timeline-editor api-url=${API}></mam-timeline-editor>`);
  await waitUntil(
    () => el.querySelector('[data-clips], [data-empty], [data-error]') !== null,
    'the timeline was not shown',
  );
  return el;
}

export const order = (el: Element) => [...el.querySelectorAll('[data-clip]')].map((n) => n.getAttribute('data-clip'));
export const text = (el: Element, selector: string) => el.querySelector(selector)?.textContent?.trim();
