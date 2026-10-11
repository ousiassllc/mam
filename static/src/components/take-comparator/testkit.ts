import { fixture, html, waitUntil } from '@open-wc/testing';
import './mam-take-comparator';
import type { MamTakeComparator, TakeScore } from './mam-take-comparator';

export const API = '/api/v1/shots/s1/takes';

export function take(id: string, label: string, over: Partial<TakeScore> = {}): TakeScore {
  return {
    id,
    label,
    status: 'candidate',
    status_label: '候補',
    adopted: false,
    model_name: 'kling-3.0',
    resolution: '1080p',
    fps: 25,
    duration_seconds: 4,
    scores: null,
    proxied: false,
    video_url: `/takes/${id}/video`,
    thumbnail_url: `/takes/${id}/thumbnail`,
    judge_result_url: `/api/v1/takes/${id}/judge-result`,
    select_url: `/takes/${id}/select`,
    reject_url: `/takes/${id}`,
    partial_usable: true,
    ...over,
  };
}

export const scored = { product: 65, acting: 100, camera: 85, continuity: 100, overall: 87.5 };

export function body(
  takes: TakeScore[],
  partialUrl: string | null = '/shots/s1/takes/partial',
  limits = { max_segments: 8, min_segment_seconds: 0.3 },
) {
  return { body: { shot_id: 's1', duration_seconds: 4, partial_url: partialUrl ?? undefined, limits, takes } };
}

/** Replaces window.confirm by an answer and records the questions. */
export function stubConfirm(answer: boolean) {
  const original = window.confirm;
  const messages: string[] = [];
  window.confirm = (message?: string) => {
    messages.push(message ?? '');
    return answer;
  };
  return { messages, restore: () => (window.confirm = original) };
}

export async function comparator(): Promise<MamTakeComparator> {
  const el = await fixture<MamTakeComparator>(html`<mam-take-comparator api-url=${API}></mam-take-comparator>`);
  await waitUntil(() => el.querySelector('[data-scores], [data-empty]') !== null, 'the Takes were not shown');
  return el;
}

/** Drags on the lane of a Take from x0 to x1 (as fractions of the lane width). */
export function dragLane(el: HTMLElement, takeId: string, x0: number, x1: number): void {
  const track = el.querySelector(`[data-lane="${takeId}"] [data-track]`) as HTMLElement;
  const rect = track.getBoundingClientRect();
  const at = (f: number) => ({
    clientX: rect.left + rect.width * f,
    clientY: rect.top + 3,
    pointerId: 1,
    bubbles: true,
  });
  track.dispatchEvent(new PointerEvent('pointerdown', at(x0)));
  track.dispatchEvent(new PointerEvent('pointermove', at((x0 + x1) / 2)));
  track.dispatchEvent(new PointerEvent('pointermove', at(x1)));
  track.dispatchEvent(new PointerEvent('pointerup', at(x1)));
}

export const text = (el: Element, selector: string) => el.querySelector(selector)?.textContent?.trim();
