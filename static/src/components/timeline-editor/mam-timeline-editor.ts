import { LitElement, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { get, put } from '../lib/api';
import {
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
  type TransitionType,
} from '../lib/edit-model';
import { errorMessage } from '../lib/errors';
import { createLogger } from '../lib/logger';
import '../video-player/mam-video-player';
import { type EditTimelineBody, TIMELINE_RELOAD_EVENT, TIMELINE_UPDATED_EVENT } from './timeline-api';
import { renderClipRow } from './timeline-clip';

const log = createLogger('timeline-editor');

/**
 * `<mam-timeline-editor>`: the Edit Timeline of a Project (spec/components/overview.md §4.2, F-18 Simple).
 *
 * - Shot Selection (drag or arrows to reorder, "外す" leaves a clip out) and Cut Point / Timing (In / Out second of the Take).
 * - Transition: how a clip follows the one before it (cut / fade / fade to black / dissolve).
 * - A preview player of the rendered Rough Cut of the saved version.
 *
 * `api-url` is GET /api/v1/projects/:id/edit-timeline. Saving PUTs the clips to the `save_url` of the response; the
 * server keeps every save as a new version. Once saved the component fires `timeline-updated` (bubbles, composed;
 * detail: {id}) so that HTMX refreshes what depends on the timeline, and reloads. It also reloads on `timeline-reload`
 * (the server's actions send it when the timeline or its editability changed; unsaved edits survive). Light DOM: the
 * page's Tailwind classes apply.
 */
@customElement('mam-timeline-editor')
export class MamTimelineEditor extends LitElement {
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';

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
  private readonly onReload = () => void this.load({ keepEdits: true });

  protected override createRenderRoot(): HTMLElement {
    return this;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    document.addEventListener(TIMELINE_RELOAD_EVENT, this.onReload);
    if (this.hasUpdated) this.onReload(); // moved in the DOM: apiUrl did not change, so reload
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
   * Loads the latest timeline again, dropping unsaved changes (unless `keepEdits`, see below). Resolves false when the
   * request failed (the timeline on screen is then the old one); a superseded load or a 404 (no Rough Cut) is not a failure.
   * With `keepEdits` (`timeline-reload`) unsaved changes stay while the server has the version they were made on (only
   * its `readonly` etc. are taken); a newer version (a new Rough Cut) drops them, and the notice says so.
   */
  async load({ keepEdits = false }: { keepEdits?: boolean } = {}): Promise<boolean> {
    const token = ++this.loadToken;
    if (!this.apiUrl) return true;
    let ok = true;
    try {
      const body = await get<EditTimelineBody>(this.apiUrl);
      if (token !== this.loadToken) return true;
      const previous = this.body;
      const unsaved = keepEdits && previous !== null && isDirty(previous.clips, this.clips);
      this.body = body;
      if (!(unsaved && previous.version_number === body.version_number)) {
        this.clips = withStarts(body.clips);
        this.captionDraft = null;
        if (unsaved) this.notice = '最新の版に差し替わったため、未保存の変更は破棄されました';
      }
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
    return (this.body?.readonly ?? true) || this.busy;
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
    if (problem) {
      this.refuse(clip, problem);
      return;
    }
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
      this.refuse(clip, problem);
      return;
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
      this.dispatchEvent(
        new CustomEvent(TIMELINE_UPDATED_EVENT, { detail: { id: this.body?.id ?? '' }, bubbles: true, composed: true }),
      );
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
          ${body.readonly ? html`<span class="text-xs text-amber-300" data-readonly>この段階では編集できません</span>` : nothing}
        </div>
      </div>
    `;
  }

  private renderError() {
    return this.error
      ? html`<p class="rounded border border-red-500/50 bg-red-950/40 px-3 py-2 text-xs text-red-200" role="alert" data-error>${this.error}</p>`
      : nothing;
  }

  private renderClip(c: EditClip, i: number, body: EditTimelineBody) {
    return renderClipRow(c, i, body, {
      locked: this.locked,
      over: this.overIndex === i && this.dragIndex !== i,
      dragging: this.dragIndex === i,
      count: this.clips.length,
      captionDraft: this.captionDraft?.index === i ? this.captionDraft.text : null,
      dragStart: (at, e) => {
        this.dragIndex = at;
        e.dataTransfer?.setData('text/plain', String(at));
        if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
      },
      dragOver: (at, e) => {
        if (this.dragIndex < 0) return;
        e.preventDefault();
        this.overIndex = at;
      },
      drop: (at) => this.onDrop(at),
      dragEnd: () => (this.dragIndex = this.overIndex = -1),
      move: (from, to) => this.change(moveClip(this.clips, from, to)),
      remove: (at) => this.change(removeClip(this.clips, at)),
      trim: (at, field, input) => this.onTrim(at, field, input),
      transition: (at, type, raw) => this.onTransition(at, type, raw),
      caption: (at, input) => this.onCaption(at, input),
    });
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'mam-timeline-editor': MamTimelineEditor;
  }
}
