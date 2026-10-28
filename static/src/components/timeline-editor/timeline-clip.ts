import { html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import { CLIP_KIND_LABEL, TRANSITION_LABEL, type EditClip, type TransitionType } from '../lib/edit-model';
import type { EditTimelineBody } from './timeline-api';

/** What a clip row shows and does: the editor owns the clips, the drag and the requests; the row only renders. */
export interface ClipRowView {
  locked: boolean;
  /** A clip is dragged over this row. */
  over: boolean;
  /** This row is the one dragged. */
  dragging: boolean;
  /** The number of clips (the last one cannot be moved down, the only one cannot be left out). */
  count: number;
  /** The caption text a refused edit left in the box, or null. */
  captionDraft: string | null;
  dragStart(i: number, e: DragEvent): void;
  dragOver(i: number, e: DragEvent): void;
  drop(i: number): void;
  dragEnd(): void;
  move(from: number, to: number): void;
  remove(i: number): void;
  trim(i: number, field: 'in' | 'out', input: HTMLInputElement): void;
  transition(i: number, type: TransitionType, raw?: string): void;
  caption(i: number, input: HTMLTextAreaElement): void;
}

/** The key of a row: the clip itself, so that the DOM (and the focus in it) moves with the clip when the clips are reordered. */
export const clipKey = (c: EditClip): string => c.shot_id;

export function renderClipRow(c: EditClip, i: number, body: EditTimelineBody, view: ClipRowView) {
  const { locked } = view;
  const t = c.transition;
  return html`<li
    class="flex flex-col gap-2 rounded border p-3 ${view.over ? 'border-indigo-400' : 'border-neutral-700'} ${view.dragging ? 'opacity-50' : ''}"
    data-clip=${c.shot_id}
    draggable=${locked ? 'false' : 'true'}
    @dragstart=${(e: DragEvent) => view.dragStart(i, e)}
    @dragover=${(e: DragEvent) => view.dragOver(i, e)}
    @drop=${(e: DragEvent) => {
      e.preventDefault();
      view.drop(i);
    }}
    @dragend=${() => view.dragEnd()}
  >
    <div class="flex flex-wrap items-center gap-3">
      <span class="text-xs text-neutral-400">${i + 1}</span>
      ${c.thumbnail_url ? html`<img class="h-10 w-auto rounded" src=${c.thumbnail_url} alt="" />` : nothing}
      <div class="min-w-40 flex-1">
        <p class="text-sm font-medium">
          ${c.label}${c.kind && c.kind !== 'main' ? html` <span class="rounded bg-sky-500/20 px-1.5 py-0.5 text-[10px] text-sky-300" data-kind=${c.kind}>${CLIP_KIND_LABEL[c.kind]}</span>` : nothing}
        </p>
        ${c.objective ? html`<p class="text-xs text-neutral-400">${c.objective}</p>` : nothing}
      </div>
      <span class="text-xs text-neutral-400" data-start>${c.start.toFixed(1)} 秒〜 / ${(c.out_point - c.in_point).toFixed(1)} 秒</span>
      <button type="button" aria-label="${c.label} を上へ" data-action="up" class="px-1 text-neutral-400 hover:text-neutral-100 disabled:opacity-30" ?disabled=${locked || i === 0} @click=${() => view.move(i, i - 1)}>↑</button>
      <button type="button" aria-label="${c.label} を下へ" data-action="down" class="px-1 text-neutral-400 hover:text-neutral-100 disabled:opacity-30" ?disabled=${locked || i === view.count - 1} @click=${() => view.move(i, i + 1)}>↓</button>
      <button type="button" aria-label="外す: ${c.label}" data-action="remove" class="text-xs text-neutral-400 underline disabled:opacity-30" ?disabled=${locked || view.count <= 1} @click=${() => view.remove(i)}>外す</button>
    </div>
    <div class="flex flex-wrap items-end gap-3 text-xs text-neutral-400">
      <label class="flex flex-col gap-1">
        開始（秒）
        <input type="number" data-field="in" step="0.1" min="0" class="w-24 rounded border border-neutral-500 bg-neutral-900 px-2 py-1 text-neutral-100" .value=${live(String(c.in_point))} ?disabled=${locked} @change=${(e: Event) => view.trim(i, 'in', e.target as HTMLInputElement)} />
      </label>
      <label class="flex flex-col gap-1">
        終了（秒）
        <input type="number" data-field="out" step="0.1" min="0" class="w-24 rounded border border-neutral-500 bg-neutral-900 px-2 py-1 text-neutral-100" .value=${live(String(c.out_point))} ?disabled=${locked} @change=${(e: Event) => view.trim(i, 'out', e.target as HTMLInputElement)} />
      </label>
      ${c.source_seconds ? html`<span>Take の長さ ${c.source_seconds.toFixed(1)} 秒</span>` : nothing}
      ${
        i === 0
          ? nothing
          : html`<label class="flex flex-col gap-1">
              前のクリップからの切り替え
              <select data-field="transition" class="rounded border border-neutral-500 bg-neutral-900 px-2 py-1 text-neutral-100" ?disabled=${locked} @change=${(e: Event) => view.transition(i, (e.target as HTMLSelectElement).value as TransitionType)}>
                ${body.transitions.map((v) => html`<option value=${v} .selected=${live(v === t.type)}>${TRANSITION_LABEL[v] ?? v}</option>`)}
              </select>
            </label>
            ${
              t.type === 'cut'
                ? nothing
                : html`<label class="flex flex-col gap-1">
                  長さ（秒）
                  <input type="number" data-field="transition-seconds" step="0.05" min="0.1" max=${body.limits.max_transition_seconds} class="w-24 rounded border border-neutral-500 bg-neutral-900 px-2 py-1 text-neutral-100" .value=${live(String(t.seconds))} ?disabled=${locked} @change=${(e: Event) => view.transition(i, t.type, (e.target as HTMLInputElement).value)} />
                </label>`
            }`
      }
    </div>
    <label class="flex flex-col gap-1 text-xs text-neutral-400">
      字幕（Caption）
      <textarea
        data-field="caption"
        rows="2"
        maxlength=${body.limits.max_caption_runes * 2}
        placeholder="この区間に重ねる文字。空なら字幕なし"
        class="rounded border border-neutral-500 bg-neutral-900 px-2 py-1 text-neutral-100"
        .value=${live(view.captionDraft ?? c.caption ?? '')}
        ?disabled=${locked}
        @change=${(e: Event) => view.caption(i, e.target as HTMLTextAreaElement)}
      ></textarea>
    </label>
  </li>`;
}
