import { html } from 'lit';
import type { TakeScore } from './take-model';

/** One score of the table: a dash while the Take has none, red when it is below `minAxisScore` (the server's, the one the Judge fails a Take under). */
export function renderScore(value: number | undefined, minAxisScore: number) {
  return value === undefined
    ? html`<span class="text-neutral-400">—</span>`
    : html`<span class=${value < minAxisScore ? 'text-red-300' : ''}>${Math.round(value)}</span>`;
}

/** The line under a Take's name: model, resolution, frame rate, and whether it plays through a proxy. */
export function describeTake(t: Pick<TakeScore, 'model_name' | 'resolution' | 'fps' | 'proxied'>): string {
  return [t.model_name, t.resolution, t.fps ? `${t.fps.toFixed(0)}fps` : '', t.proxied ? '4K（プロキシで再生）' : '']
    .filter(Boolean)
    .join(' · ');
}
