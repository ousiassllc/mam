import { LitElement, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { del, get, post } from '../lib/api';
import { errorMessage } from '../lib/errors';
import { createLogger } from '../lib/logger';
import {
  addSegment,
  describeSegments,
  dragToSegment,
  removeSegment,
  totalSeconds,
  type Clip,
  type Segment,
} from '../lib/timeline-model';
import { DEFAULT_FPS } from '../lib/video';
import '../video-player/mam-video-player';
import type { TakeScore, TakesBody } from './take-model';
import { renderTakeTable } from './take-table';

const log = createLogger('take-comparator');

/** The same wording as the Take row of the Shot card (`shot_card.templ`, `hx-confirm`): rejecting cannot be undone, so every entry point asks. */
const REJECT_CONFIRM = 'この Take を除外しますか？';

/** A drag on a lane in progress. */
interface Drag {
  clip: Clip;
  fromX: number;
  toX: number;
  width: number;
}

/**
 * `<mam-take-comparator>`: the Takes of a Shot side by side (spec/components/overview.md §4.3, UC-3).
 *
 * - A score table: Product / Acting / Camera / Continuity and Status (the server's `status_label`), with the buttons that select or exclude a Take.
 * - A player and a lane per Take. Dragging on a lane picks that part of the Take; the parts are joined
 *   in the order they were picked (Partial Take Use). The same can be written in words:
 *   "Take A の 0-2秒 + Take B の 2-4秒".
 *
 * `api-url` is GET /api/v1/shots/:id/takes. Once the server has joined the parts the component fires
 * `partial-take-confirmed` (bubbles, composed; detail: {segments}) and reloads the Takes.
 * It renders into the light DOM so that the page's Tailwind classes apply.
 */
@customElement('mam-take-comparator')
export class MamTakeComparator extends LitElement {
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';

  @state() private takes: TakeScore[] = [];
  @state() private selection: Segment[] = [];
  @state() private instruction = '';
  @state() private drag: Drag | null = null;
  @state() private busy = false;
  @state() private error = '';
  @state() private loaded = false;

  private shotDuration = 0;
  private partialUrl = '';
  private limits: TakesBody['limits'] | null = null;
  private loadToken = 0;

  protected override createRenderRoot(): HTMLElement {
    return this;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    if (this.hasUpdated) void this.load(); // moved in the DOM: the properties did not change, so reload
  }

  protected override updated(changed: Map<string, unknown>): void {
    if (changed.has('apiUrl')) void this.load();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.loadToken += 1;
  }

  /** Loads the Takes again (after a choice, or a composition). */
  async load(): Promise<void> {
    const token = ++this.loadToken;
    if (!this.apiUrl) return;
    try {
      const body = await get<TakesBody>(this.apiUrl);
      if (token !== this.loadToken) return;
      this.takes = body.takes;
      this.shotDuration = body.duration_seconds ?? 0;
      this.partialUrl = body.partial_url ?? '';
      this.limits = body.limits;
      const usable = new Set(body.takes.filter((t) => t.partial_usable).map((t) => t.id));
      this.selection = this.selection.filter((s) => usable.has(s.take_id));
      this.error = '';
    } catch (error) {
      if (token === this.loadToken) this.error = errorMessage(error);
      log.warn('could not load the Takes', error);
    } finally {
      if (token === this.loadToken) this.loaded = true;
    }
  }

  private clips(): Clip[] {
    return this.takes
      .filter((t) => t.partial_usable)
      .map((t) => ({
        id: t.id,
        label: t.label,
        duration: t.duration_seconds ?? 0,
        fps: t.fps && t.fps > 0 ? t.fps : DEFAULT_FPS,
      }));
  }

  /** Rejects a Take once the user confirmed: the Take can never be chosen again. */
  private reject(url: string): void {
    if (window.confirm(REJECT_CONFIRM)) void this.act(() => del(url));
  }

  /** Runs a request to the server, reports its error and reloads the Takes. */
  private async act(run: () => Promise<unknown>): Promise<boolean> {
    if (this.busy) return false;
    this.busy = true;
    this.error = '';
    try {
      await run();
      await this.load();
      return true;
    } catch (error) {
      this.error = errorMessage(error);
      return false;
    } finally {
      this.busy = false;
    }
  }

  private async confirmPartial(body: { segments: Segment[] } | { instruction: string }): Promise<void> {
    const ok = await this.act(() => post(this.partialUrl, body));
    if (!ok) return;
    const segments = 'segments' in body ? body.segments : [];
    this.selection = [];
    this.instruction = '';
    this.dispatchEvent(
      new CustomEvent('partial-take-confirmed', { detail: { segments }, bubbles: true, composed: true }),
    );
  }

  private onLaneDown(event: PointerEvent, clip: Clip): void {
    const lane = event.currentTarget as HTMLElement;
    const rect = lane.getBoundingClientRect();
    if (rect.width === 0) return;
    lane.setPointerCapture?.(event.pointerId);
    const x = event.clientX - rect.left;
    this.drag = { clip, fromX: x, toX: x, width: rect.width };
  }

  private onLaneMove(event: PointerEvent): void {
    if (!this.drag) return;
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    this.drag = { ...this.drag, toX: event.clientX - rect.left };
  }

  private onLaneUp(): void {
    const drag = this.drag;
    this.drag = null;
    if (!drag || !this.limits) return;
    const segment = dragToSegment(drag.clip, drag.fromX, drag.toX, drag.width);
    const next = addSegment(this.selection, segment, this.clips(), this.limits);
    if (next === this.selection) {
      this.error =
        this.selection.length >= this.limits.max_segments
          ? `区間は最大 ${this.limits.max_segments} 個までです`
          : `${this.limits.min_segment_seconds} 秒以上の区間をドラッグしてください`;
      return;
    }
    this.error = '';
    this.selection = next;
  }

  protected override render() {
    if (!this.loaded) return html`<p class="text-sm text-neutral-400" role="status">読み込み中…</p>`;
    if (this.takes.length === 0) {
      return html`${this.renderError()}<p class="text-sm text-neutral-400" data-empty>この Shot にはまだ Take がありません</p>`;
    }
    const clips = this.clips();
    const clipById = new Map(clips.map((c) => [c.id, c]));
    const canMerge = this.selection.length >= 2 && !this.busy;
    return html`
      <div class="flex flex-col gap-4">
        ${this.renderError()}
        <div class="overflow-x-auto">${renderTakeTable(this.takes, this.limits?.min_axis_score ?? 0, {
          busy: this.busy,
          select: (url) => void this.act(() => post(url)),
          reject: (url) => this.reject(url),
        })}</div>
        ${
          this.partialUrl
            ? html`<section class="flex flex-col gap-3" aria-label="区間指定">
          <h3 class="text-sm font-semibold">区間で採用（Partial Take Use）</h3>
          ${this.takes.map((t) => this.renderLane(t, clipById))}
          <div class="flex flex-col gap-2 rounded border border-neutral-700 p-3" data-selection>
            ${
              this.selection.length === 0
                ? html`<p class="text-xs text-neutral-400">Take の帯をドラッグして区間を選ぶと、選んだ順につなぎます。</p>`
                : html`<ol class="flex flex-wrap gap-2 text-xs">
                    ${this.selection.map(
                      (s, i) => html`<li class="flex items-center gap-1 rounded bg-neutral-800 px-2 py-1" data-segment>
                        <span>${describeSegments([s], clips)}</span>
                        <button type="button" class="text-neutral-400 hover:text-neutral-100" aria-label="区間 ${i + 1} を外す" data-action="remove-segment" @click=${() => (this.selection = removeSegment(this.selection, i))}>×</button>
                      </li>`,
                    )}
                  </ol>
                  <p class="text-xs text-neutral-400">合計 ${totalSeconds(this.selection).toFixed(1)} 秒（つなぎ目はクロスフェード）</p>`
            }
            <div class="flex flex-wrap items-center gap-2">
              <button type="button" data-action="confirm-partial" class="rounded bg-neutral-200 px-3 py-1 text-xs font-medium text-neutral-900 disabled:opacity-40" ?disabled=${!canMerge} @click=${() => void this.confirmPartial({ segments: this.selection })}>
                この区間で合成
              </button>
              <button type="button" class="text-xs text-neutral-400 underline" ?disabled=${this.selection.length === 0} @click=${() => (this.selection = [])}>選び直す</button>
            </div>
            <form class="flex flex-wrap items-center gap-2" @submit=${this.onInstructionSubmit}>
              <input
                type="text"
                name="instruction"
                class="min-w-64 flex-1 rounded border border-neutral-500 bg-neutral-900 px-2 py-1 text-xs"
                placeholder="Take A の 0-2秒 + Take B の 2-4秒"
                aria-label="区間を言葉で指定"
                .value=${this.instruction}
                @input=${(e: Event) => (this.instruction = (e.target as HTMLInputElement).value)}
              />
              <button type="submit" data-action="confirm-instruction" class="rounded border border-neutral-600 px-3 py-1 text-xs disabled:opacity-40" ?disabled=${this.busy || this.instruction.trim() === ''}>
                言葉で合成
              </button>
            </form>
          </div>
        </section>`
            : nothing
        }
      </div>
    `;
  }

  private onInstructionSubmit(event: Event): void {
    event.preventDefault();
    const instruction = this.instruction.trim();
    if (instruction && !this.busy) void this.confirmPartial({ instruction });
  }

  private renderError() {
    return this.error
      ? html`<p class="rounded border border-red-500/50 bg-red-950/40 px-3 py-2 text-xs text-red-200" role="alert" data-error>${this.error}</p>`
      : nothing;
  }

  private renderLane(t: TakeScore, clipById: Map<string, Clip>) {
    const clip = clipById.get(t.id);
    if (!clip || !t.video_url) return nothing;
    const duration = clip.duration;
    const axis = Math.max(...[...clipById.values()].map((c) => c.duration), this.shotDuration, 0.1);
    const widthPercent = duration > 0 ? Math.min(duration / axis, 1) * 100 : 100;
    const drag = this.drag && this.drag.clip.id === t.id ? this.drag : null;
    const preview = drag ? dragToSegment(drag.clip, drag.fromX, drag.toX, drag.width) : null;
    const bar = (s: { start: number; end: number }) =>
      `left:${(s.start / (duration || 1)) * 100}%;width:${((s.end - s.start) / (duration || 1)) * 100}%`;
    return html`<div class="flex flex-col gap-1" data-lane=${t.id}>
      <div class="text-xs text-neutral-300">Take ${t.label}</div>
      <mam-video-player
        class="block max-w-md"
        video-src=${t.video_url}
        api-url=${t.judge_result_url}
        poster=${t.thumbnail_url ?? ''}
        .fps=${t.fps ?? 0}
      ></mam-video-player>
      <div
        class="relative h-7 cursor-crosshair touch-none select-none rounded bg-neutral-800"
        style="width:${widthPercent}%"
        role="group"
        aria-label="Take ${t.label} の区間を選ぶ"
        data-track
        @pointerdown=${(e: PointerEvent) => this.onLaneDown(e, clip)}
        @pointermove=${this.onLaneMove}
        @pointerup=${this.onLaneUp}
        @pointercancel=${() => (this.drag = null)}
      >
        ${this.selection.map((s, i) =>
          s.take_id === t.id
            ? html`<div class="pointer-events-none absolute top-0 h-full rounded-sm bg-emerald-500/60 text-[10px] leading-7 text-white" style=${bar(s)} data-picked>${i + 1}</div>`
            : nothing,
        )}
        ${preview ? html`<div class="pointer-events-none absolute top-0 h-full rounded-sm bg-sky-400/50" style=${bar(preview)} data-drag></div>` : nothing}
      </div>
    </div>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'mam-take-comparator': MamTakeComparator;
  }
}
