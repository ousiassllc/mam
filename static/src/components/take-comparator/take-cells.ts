import { html } from 'lit';
import type { TakeScore } from './mam-take-comparator';

/** A score below this is shown in red in the score table. */
const LOW_SCORE = 60;

/** One score of the table: a dash while the Take has none, red when it is low. */
export function renderScore(value: number | undefined) {
  return value === undefined
    ? html`<span class="text-neutral-500">—</span>`
    : html`<span class=${value < LOW_SCORE ? 'text-red-300' : ''}>${Math.round(value)}</span>`;
}

/** The line under a Take's name: model, resolution, frame rate, and whether it plays through a proxy. */
export function describeTake(t: Pick<TakeScore, 'model_name' | 'resolution' | 'fps' | 'proxied'>): string {
  return [t.model_name, t.resolution, t.fps ? `${t.fps.toFixed(0)}fps` : '', t.proxied ? '4K（プロキシで再生）' : '']
    .filter(Boolean)
    .join(' · ');
}
