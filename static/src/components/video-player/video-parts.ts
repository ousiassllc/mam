import { html, nothing } from 'lit';
import { describeProgress, progressFraction, type ShotProgress } from '../lib/shot';
import { formatTimecode, issueRange, type JudgeIssue, type TimeRange } from '../lib/video';

/** An Issue as the time bar draws it. */
export interface Marker {
  issue: JudgeIssue;
  range: TimeRange;
}

/** The Issues that have a time range at `rate` (0: the rate is a guess, so only the ranges in seconds count). */
export function buildMarkers(issues: JudgeIssue[], rate: number): Marker[] {
  const out: Marker[] = [];
  for (const issue of issues) {
    const range = issueRange(issue, rate);
    if (range) out.push({ issue, range });
  }
  return out;
}

/** What the time bar and the transport buttons show and do: the player owns the video and its state. */
export interface TransportView {
  /** Duration in seconds (0 until the metadata is loaded). */
  max: number;
  rate: number;
  rateKnown: boolean;
  currentTime: number;
  playing: boolean;
  markers: Marker[];
  scrub(event: Event): void;
  step(delta: number): void;
  toggle(): void;
  seek(time: number): void;
}

function renderMarker(marker: Marker, max: number, seek: (time: number) => void) {
  const { range, issue } = marker;
  const left = Math.min(range.start / max, 1) * 100;
  const width = Math.max(Math.min((range.end - range.start) / max, 1 - left / 100) * 100, 0.8);
  const color =
    issue.severity === 'high' ? 'bg-red-500/70' : issue.severity === 'medium' ? 'bg-amber-500/70' : 'bg-sky-500/70';
  const label = `${issue.type}（${issue.severity}） ${range.start.toFixed(1)}-${range.end.toFixed(1)}秒`;
  return html`<button
    type="button"
    data-marker
    data-severity=${issue.severity}
    class="pointer-events-auto absolute top-0 h-full rounded-sm ${color}"
    style="left:${left}%;width:${width}%"
    title=${label}
    aria-label=${label}
    @click=${() => seek(range.start)}
  ></button>`;
}

/** The time bar (with the Issue markers) and the frame / play buttons under the video. */
export function renderTransport(v: TransportView) {
  const { max, rate } = v;
  const playLabel = v.playing ? '一時停止' : '再生';
  return html`
    <div class="relative" data-timebar>
      <input
        type="range"
        class="w-full"
        aria-label="再生位置"
        min="0"
        max=${max}
        step=${1 / rate}
        .value=${String(v.currentTime)}
        ?disabled=${max === 0}
        @input=${v.scrub}
      />
      ${
        max > 0
          ? html`<div class="pointer-events-none absolute inset-x-0 top-0 h-full">
            ${v.markers.map((m) => renderMarker(m, max, v.seek))}
          </div>`
          : nothing
      }
    </div>
    <div class="flex items-center gap-2 text-xs text-neutral-300">
      <button type="button" data-action="prev-frame" class="rounded border border-neutral-700 px-2 py-0.5" aria-label="1 コマ戻る" title="1 コマ戻る" @click=${() => v.step(-1)}>◀|</button>
      <button type="button" data-action="play" class="rounded border border-neutral-700 px-2 py-0.5" aria-label=${playLabel} title=${playLabel} @click=${v.toggle}>
        ${v.playing ? '❚❚' : '▶'}
      </button>
      <button type="button" data-action="next-frame" class="rounded border border-neutral-700 px-2 py-0.5" aria-label="1 コマ進む" title="1 コマ進む" @click=${() => v.step(1)}>|▶</button>
      <span class="font-mono" data-timecode>${formatTimecode(v.currentTime, rate)}</span>
      <span class="font-mono text-neutral-400">/ ${formatTimecode(max, rate)}</span>
      <span class="text-neutral-400">${rate.toFixed(rate % 1 === 0 ? 0 : 2)} fps${v.rateKnown ? '' : '（仮）'}</span>
    </div>
  `;
}

/** Stands in for the video before it exists or while it is being made: the progress of the generation. */
export function renderPlaceholder(progress: ShotProgress | null) {
  const fraction = progressFraction(progress);
  return html`<div
    class="flex aspect-video flex-col items-center justify-center gap-2 rounded border border-dashed border-neutral-700 bg-neutral-900 text-xs text-neutral-400"
    role="status"
    data-placeholder
    data-stage=${progress?.stage ?? 'idle'}
  >
    <span>${describeProgress(progress)}</span>
    ${
      fraction === null
        ? nothing
        : html`<div class="h-1 w-1/2 overflow-hidden rounded bg-neutral-800">
          <div class="h-full bg-sky-500/70" style="width:${fraction * 100}%" data-progress-bar></div>
        </div>`
    }
  </div>`;
}
