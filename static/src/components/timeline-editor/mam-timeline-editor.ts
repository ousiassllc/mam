import { LitElement, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { live } from 'lit/directives/live.js';
import { get, put } from '../lib/api';
import {
  CLIP_KIND_LABEL,
  TRANSITION_LABEL,
  captionProblem,
  isDirty,
  moveClip,
  removeClip,
  setCaption,
  setTransition,
  setTrim,
  timelineSeconds,
  toPayload,
  trimProblem,
  withStarts,
  type EditClip,
  type EditLimits,
  type TransitionType,
} from '../lib/edit-model';
import { errorMessage } from '../lib/errors';
import { createLogger } from '../lib/logger';
import '../video-player/mam-video-player';

const log = createLogger('timeline-editor');

/** The additional Shot only new footage settles (Editor → Reshoot Loop). */
interface ShotNeed {
  kind: 'insert' | 'reaction';
  after_clip: number;
  objective: string;
  duration_seconds: number;
}

interface EditorFinding {
  clip_index: number;
  issue: string;
  action: string;
  need_shot?: ShotNeed;
}

/** GET /api/v1/projects/:id/edit-timeline. The URLs are the server's: the component builds none. */
export interface EditTimelineBody {
  id: string;
  version_number: number;
  duration_seconds: number;
  clips: EditClip[];
  judge?: { verdict: 'pass' | 're_edit' | 'need_shot'; findings: EditorFinding[] };
  branch_id?: string;
  save_url: string;
  /** POST here (an action route) to ask the Director for the additional Shots of a need_shot verdict. */
  request_shot_url: string;
  preview_url: string;
  transitions: TransitionType[];
  limits: EditLimits;
  readonly: boolean;
}

/** Fired when the server made new work of its own (the Reshoot Loop was started): the page's stage buttons reload. */
export const TIMELINE_UPDATED_EVENT = 'timeline-updated';

/** The event `htmx` fires (HX-Trigger of the Rough Cut / Final QC actions) when the timeline changed on the server. */
export const TIMELINE_RELOAD_EVENT = 'timeline-reload';

/**
 * `<mam-timeline-editor>`: the Edit Timeline of a Project (spec/components/overview.md §4.2, F-18 Simple).
 *
 * - Shot Selection: drag a clip (or use the arrows) to reorder; "外す" leaves a clip out.
 * - Cut Point / Timing: the In and Out second of the Take of each clip.
 * - Transition: how a clip follows the one before it (cut / fade / fade to black / dissolve).
 * - A preview player of the rendered Rough Cut of the saved version.
 *
 * `api-url` is GET /api/v1/projects/:id/edit-timeline. Saving PUTs the clips to the `save_url` of the
 * response; the server keeps every save as a new version. Once saved the component fires
 * `timeline-updated` (bubbles, composed; detail: {id}) so that HTMX refreshes what depends on the
 * timeline, and reloads. It also reloads on `timeline-reload`, which the server's actions send when
 * they replaced the timeline (a new Rough Cut). It renders into the light DOM so that the page's
 * Tailwind classes apply.
 */
@customElement('mam-timeline-editor')
export class MamTimelineEditor extends LitElement {
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';
  @property({ type: Boolean }) readonly = false;

  @state() private body: EditTimelineBody | null = null;
  @state() private clips: EditClip[] = [];
  @state() private error = '';
  @state() private notice = '';
  @state() private busy = false;
  @state() private loaded = false;
  @state() private dragIndex = -1;
  @state() private overIndex = -1;

  private captionDraft: { index: number; text: string } | null = null;
  private loadToken = 0;
  private readonly onReload = () => void this.load();

  protected override createRenderRoot(): HTMLElement {
    return this;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    document.addEventListener(TIMELINE_RELOAD_EVENT, this.onReload);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    document.removeEventListener(TIMELINE_RELOAD_EVENT, this.onReload);
    this.loadToken += 1;
  }

  protected override updated(changed: Map<string, unknown>): void {
    if (changed.has('apiUrl')) void this.load();
  }

  /**
   * Loads the latest timeline again, dropping unsaved changes. Resolves false when the request failed (the timeline
   * on screen is then the old one); a load that a newer one superseded, or a 404 (no Rough Cut), is not a failure.
   */
  async load(): Promise<boolean> {
    const token = ++this.loadToken;
    if (!this.apiUrl) return true;
    let ok = true;
    try {
      const body = await get<EditTimelineBody>(this.apiUrl);
      if (token !== this.loadToken) return true;
      this.body = body;
      this.clips = withStarts(body.clips);
      this.captionDraft = null;
      this.error = '';
    } catch (error) {
      if (token !== this.loadToken) return true;
      if ((error as { status?: number }).status === 404) {
        // There is no Rough Cut (any more), which is not an error to shout about.
        this.body = null;
        this.clips = [];
        this.captionDraft = null;
        this.error = '';
      } else {
        // Any other failure says nothing about the timeline: keep the one on screen (and its unsaved edits).
        this.error = errorMessage(error);
        ok = false;
      }
      log.warn('could not load the Edit Timeline', error);
    } finally {
      if (token === this.loadToken) this.loaded = true;
    }
    return ok;
  }

  private get locked(): boolean {
    return this.readonly || (this.body?.readonly ?? true) || this.busy;
  }

  /**
   * Applies an edit. Every field is bound with `live()` (it compares with the DOM, not with the last render), so a
   * re-render also writes the model's value back over what the user typed: an input the model clamped or refused
   * shows the accepted value, whether or not the clips changed.
   */
  private change(next: EditClip[]): void {
    this.captionDraft = null;
    if (next !== this.clips) {
      this.clips = next;
      this.notice = '';
      this.error = '';
    }
    this.requestUpdate();
  }

  /** Refuses an edit: shows why, and (re-render) puts the last accepted value back into the field. */
  private refuse(clip: EditClip, problem: string): void {
    this.error = `${clip.label}: ${problem}`;
    this.requestUpdate();
  }

  private onTrim(index: number, field: 'in' | 'out', input: HTMLInputElement): void {
    const body = this.body;
    const clip = this.clips[index];
    if (!body || !clip) return;
    const value = Number.parseFloat(input.value);
    const inPoint = field === 'in' ? value : clip.in_point;
    const outPoint = field === 'out' ? value : clip.out_point;
    const problem = trimProblem(clip, inPoint, outPoint, body.limits);
    if (problem) return this.refuse(clip, problem);
    this.change(setTrim(this.clips, index, inPoint, outPoint, body.limits));
  }

  private onTransition(index: number, type: TransitionType, raw?: string): void {
    const body = this.body;
    const clip = this.clips[index];
    if (!body || !clip) return;
    const seconds = raw !== undefined ? Number.parseFloat(raw) : clip.transition.seconds || 0.5;
    this.change(setTransition(this.clips, index, type, Number.isFinite(seconds) ? seconds : 0.5, body.limits));
  }

  private onCaption(index: number, input: HTMLTextAreaElement): void {
    const body = this.body;
    const clip = this.clips[index];
    if (!body || !clip) return;
    const problem = captionProblem(input.value, body.limits);
    if (problem) {
      this.captionDraft = { index, text: input.value }; // the text stays in the box so it can be shortened, until the next edit or load
      return this.refuse(clip, problem);
    }
    this.change(setCaption(this.clips, index, input.value, body.limits));
  }

  private onDrop(to: number): void {
    const from = this.dragIndex;
    this.dragIndex = this.overIndex = -1;
    if (from >= 0) this.change(moveClip(this.clips, from, to));
  }

  /** Saves the clips as the next version. */
  async save(): Promise<void> {
    const body = this.body;
    if (!body || this.locked) return;
    this.busy = true;
    this.error = '';
    try {
      await put(body.save_url, toPayload(this.clips));
      this.busy = false;
      if (!(await this.load())) {
        // Saved, but the screen still shows the old version (unsaved changes, a save from it would be refused as stale): not "updated".
        this.error = `保存は完了しましたが、最新の版の再読込に失敗しました。「変更を戻す」で再読込してください（${this.error}）`;
        return;
      }
      this.notice = '保存しました';
      this.dispatchEvent(new CustomEvent(TIMELINE_UPDATED_EVENT, { detail: { id: this.body?.id ?? '' }, bubbles: true, composed: true }));
    } catch (error) {
      this.error = errorMessage(error);
    } finally {
      this.busy = false;
    }
  }

  protected override render() {
    if (!this.loaded) return html`<p class="text-sm text-neutral-400" role="status">読み込み中…</p>`;
    const body = this.body;
    if (!body) {
      // Only a 404 (no error) means "no Rough Cut yet"; a failed first load shows its error alone.
      if (this.error) return this.renderError();
      return html`<p class="text-sm text-neutral-400" data-empty>
          Rough Cut がまだありません。各 Shot の採用 Take を選んでから「Rough Cut を作成」を押してください。
        </p>`;
    }
    const dirty = isDirty(body.clips, this.clips);
    return html`
      <div class="flex flex-col gap-4">
        ${this.renderError()}
        ${this.notice ? html`<p class="text-xs text-emerald-300" role="status" data-notice>${this.notice}</p>` : nothing}
        <mam-video-player video-src=${body.preview_url}></mam-video-player>
        ${this.renderJudge(body)}
        <ol class="flex flex-col gap-2" data-clips>
          ${this.clips.map((c, i) => this.renderClip(c, i, body))}
        </ol>
        <div class="flex flex-wrap items-center gap-3">
          <button
            type="button"
            data-action="save"
            class="rounded bg-neutral-200 px-3 py-1 text-xs font-medium text-neutral-900 disabled:opacity-40"
            ?disabled=${this.locked || !dirty}
            @click=${() => void this.save()}
          >
            保存
          </button>
          <button type="button" data-action="revert" class="text-xs text-neutral-400 underline disabled:opacity-40" ?disabled=${this.busy || !dirty} @click=${() => void this.load()}>
            変更を戻す
          </button>
          <span class="text-xs text-neutral-400" data-total>合計 ${timelineSeconds(this.clips).toFixed(1)} 秒 / ${this.clips.length} クリップ${dirty ? '（未保存の変更あり）' : ''}</span>
          ${this.readonly || body.readonly ? html`<span class="text-xs text-amber-300" data-readonly>この段階では編集できません</span>` : nothing}
        </div>
      </div>
    `;
  }

  private renderError() {
    return this.error ? html`<p class="rounded border border-red-500/50 bg-red-950/40 px-3 py-2 text-xs text-red-200" role="alert" data-error>${this.error}</p>` : nothing;
  }

  private renderJudge(body: EditTimelineBody) {
    const judge = body.judge;
    if (!judge) return nothing;
    const label = judge.verdict === 'pass' ? 'PASS' : judge.verdict === 're_edit' ? 'Re-edit' : '素材不足';
    return html`<div class="rounded border border-neutral-700 p-3 text-xs" data-judge>
      <p class="font-medium">Editor Judge: ${label}</p>
      ${judge.findings.length > 0
        ? html`<ul class="mt-1 flex flex-col gap-1 text-neutral-300">
            ${judge.findings.map(
              (f) => html`<li>
                ${f.clip_index > 0 ? `クリップ ${f.clip_index}: ` : ''}${f.issue} → ${f.action}
                ${f.need_shot
                  ? html`<span class="text-amber-300" data-need-shot>
                      （追加 Shot: ${CLIP_KIND_LABEL[f.need_shot.kind]} ${f.need_shot.duration_seconds.toFixed(1)} 秒をクリップ ${f.need_shot.after_clip} の後に）
                    </span>`
                  : nothing}
              </li>`,
            )}
          </ul>`
        : nothing}
    </div>`;
  }

  private renderClip(c: EditClip, i: number, body: EditTimelineBody) {
    const locked = this.locked;
    const over = this.overIndex === i && this.dragIndex !== i;
    const t = c.transition;
    return html`<li
      class="flex flex-col gap-2 rounded border p-3 ${over ? 'border-sky-400' : 'border-neutral-700'} ${this.dragIndex === i ? 'opacity-50' : ''}"
      data-clip=${c.shot_id}
      draggable=${locked ? 'false' : 'true'}
      @dragstart=${(e: DragEvent) => {
        this.dragIndex = i;
        e.dataTransfer?.setData('text/plain', String(i));
        if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
      }}
      @dragover=${(e: DragEvent) => {
        if (this.dragIndex < 0) return;
        e.preventDefault();
        this.overIndex = i;
      }}
      @drop=${(e: DragEvent) => {
        e.preventDefault();
        this.onDrop(i);
      }}
      @dragend=${() => (this.dragIndex = this.overIndex = -1)}
    >
      <div class="flex flex-wrap items-center gap-3">
        <span class="text-xs text-neutral-500">${i + 1}</span>
        ${c.thumbnail_url ? html`<img class="h-10 w-auto rounded" src=${c.thumbnail_url} alt="" />` : nothing}
        <div class="min-w-40 flex-1">
          <p class="text-sm font-medium">
            ${c.label}${c.kind && c.kind !== 'main' ? html` <span class="rounded bg-sky-900 px-1.5 py-0.5 text-[10px] text-sky-200" data-kind=${c.kind}>${CLIP_KIND_LABEL[c.kind]}</span>` : nothing}
          </p>
          ${c.objective ? html`<p class="text-xs text-neutral-400">${c.objective}</p>` : nothing}
        </div>
        <span class="text-xs text-neutral-400" data-start>${c.start.toFixed(1)} 秒〜 / ${(c.out_point - c.in_point).toFixed(1)} 秒</span>
        <button type="button" aria-label="${c.label} を上へ" data-action="up" class="px-1 text-neutral-400 hover:text-neutral-100 disabled:opacity-30" ?disabled=${locked || i === 0} @click=${() => this.change(moveClip(this.clips, i, i - 1))}>↑</button>
        <button type="button" aria-label="${c.label} を下へ" data-action="down" class="px-1 text-neutral-400 hover:text-neutral-100 disabled:opacity-30" ?disabled=${locked || i === this.clips.length - 1} @click=${() => this.change(moveClip(this.clips, i, i + 1))}>↓</button>
        <button type="button" data-action="remove" class="text-xs text-neutral-400 underline disabled:opacity-30" ?disabled=${locked || this.clips.length <= 1} @click=${() => this.change(removeClip(this.clips, i))}>外す</button>
      </div>
      <div class="flex flex-wrap items-end gap-3 text-xs text-neutral-400">
        <label class="flex flex-col gap-1">
          開始（秒）
          <input type="number" data-field="in" step="0.1" min="0" class="w-24 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-neutral-100" .value=${live(String(c.in_point))} ?disabled=${locked} @change=${(e: Event) => this.onTrim(i, 'in', e.target as HTMLInputElement)} />
        </label>
        <label class="flex flex-col gap-1">
          終了（秒）
          <input type="number" data-field="out" step="0.1" min="0" class="w-24 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-neutral-100" .value=${live(String(c.out_point))} ?disabled=${locked} @change=${(e: Event) => this.onTrim(i, 'out', e.target as HTMLInputElement)} />
        </label>
        ${c.source_seconds ? html`<span>Take の長さ ${c.source_seconds.toFixed(1)} 秒</span>` : nothing}
        ${i === 0
          ? nothing
          : html`<label class="flex flex-col gap-1">
                前のクリップからの切り替え
                <select data-field="transition" class="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-neutral-100" ?disabled=${locked} @change=${(e: Event) => this.onTransition(i, (e.target as HTMLSelectElement).value as TransitionType)}>
                  ${body.transitions.map((v) => html`<option value=${v} .selected=${live(v === t.type)}>${TRANSITION_LABEL[v] ?? v}</option>`)}
                </select>
              </label>
              ${t.type === 'cut'
                ? nothing
                : html`<label class="flex flex-col gap-1">
                    長さ（秒）
                    <input type="number" data-field="transition-seconds" step="0.05" min="0.1" max=${body.limits.max_transition_seconds} class="w-24 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-neutral-100" .value=${live(String(t.seconds))} ?disabled=${locked} @change=${(e: Event) => this.onTransition(i, t.type, (e.target as HTMLInputElement).value)} />
                  </label>`}`}
      </div>
      <label class="flex flex-col gap-1 text-xs text-neutral-400">
        字幕（Caption）
        <textarea
          data-field="caption"
          rows="2"
          maxlength=${body.limits.max_caption_runes * 2}
          placeholder="この区間に重ねる文字。空なら字幕なし"
          class="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-neutral-100"
          .value=${live(this.captionDraft?.index === i ? this.captionDraft.text : (c.caption ?? ''))}
          ?disabled=${locked}
          @change=${(e: Event) => this.onCaption(i, e.target as HTMLTextAreaElement)}
        ></textarea>
      </label>
    </li>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'mam-timeline-editor': MamTimelineEditor;
  }
}
