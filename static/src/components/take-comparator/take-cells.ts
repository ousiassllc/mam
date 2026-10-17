import { html } from 'lit';
import type { TakeScore } from './take-model';

/**
 * One score of the table. Colour is never the only cue (WCAG 1.4.1): a score below `minAxisScore` (the server's, the
 * one the Judge fails a Take under) is red and also carries a ▼ and a spoken "below the minimum"; a Take with no
 * score yet shows a dash that is read as "no score".
 */
export function renderScore(value: number | undefined, minAxisScore: number) {
  if (value === undefined) {
    return html`<span class="text-neutral-400" data-no-score><span aria-hidden="true">—</span><span class="sr-only">スコアなし</span></span>`;
  }
  return value < minAxisScore
    ? html`<span class="text-red-300" data-below-min>${Math.round(value)}<span class="sr-only">（基準 ${minAxisScore} 未満）</span><span aria-hidden="true"> ▼</span></span>`
    : html`<span>${Math.round(value)}</span>`;
}

/** The line under a Take's name: model, resolution, frame rate, and whether it plays through a proxy. */
export function describeTake(t: Pick<TakeScore, 'model_name' | 'resolution' | 'fps' | 'proxied'>): string {
  return [t.model_name, t.resolution, t.fps ? `${t.fps.toFixed(0)}fps` : '', t.proxied ? '4K（プロキシで再生）' : '']
    .filter(Boolean)
    .join(' · ');
}
