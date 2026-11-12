import { LitElement, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { repeat } from 'lit/directives/repeat.js';
import { appendAlert } from '../alert/alert';
import { confirmDialog } from '../confirm/confirm';
import { get, put } from '../lib/api';
import {
  captionProblem,
  isDirty,
  moveClip,
  removeClip,
  setCaption,
  setTransition,
  setTrim,
  toPayload,
  trimProblem,
  withStarts,
  type EditClip,
  type TransitionType,
} from '../lib/edit-model';
import { errorMessage } from '../lib/errors';
import { createLogger } from '../lib/logger';
import { showToast } from '../toast/toast';
import '../video-player/mam-video-player';
import { type EditTimelineBody, TIMELINE_RELOAD_EVENT, TIMELINE_UPDATED_EVENT } from './timeline-api';
import { clipKey, renderClipRow } from './timeline-clip';
import { focusMoved, focusRemoved, focusToolbar } from './timeline-focus';
import { renderToolbar } from './timeline-toolbar';

const log = createLogger('timeline-editor');

const REVERT_CONFIRM = '未保存の変更を破棄して、最新の版を読み直しますか？';
const DISCARDED = '最新の版に差し替わったため、未保存の変更は破棄されました';

/** What the user has to read again goes to the page's #alert-region (spec/components/lit-widgets-guide.md W8); the same text is not piled up. */
function alertUser(message: string, kind: 'error' | 'warning' = 'error'): void {
  appendAlert(document.getElementById('alert-region'), message, kind);
}

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
  @state() private announce = '';
  /** The first load failed (the alert says why): there is nothing to show, and it is not "no Rough Cut yet". */
  @state() private loadFailed = false;
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
   * its `readonly` etc. are taken); a newer version (a new Rough Cut) drops them, and a warning alert says so.
   * A failure is shown as an alert here.
   */
  async load(options: { keepEdits?: boolean } = {}): Promise<boolean> {
    const failure = await this.fetchLatest(options);
    if (failure !== null) alertUser(failure);
    return failure === null;
  }

  /** `load` without showing the failure: the message of the failure, or null when it went well. */
  private async fetchLatest({ keepEdits = false }: { keepEdits?: boolean } = {}): Promise<string | null> {
    const token = ++this.loadToken;
    if (!this.apiUrl) return null;
    let failure: string | null = null;
    try {
      const body = await get<EditTimelineBody>(this.apiUrl);
      if (token !== this.loadToken) return null;
      const previous = this.body;
      const unsaved = keepEdits && previous !== null && isDirty(previous.clips, this.clips);
      this.body = body;
      if (!(unsaved && previous.version_number === body.version_number)) {
        this.clips = withStarts(body.clips);
        this.captionDraft = null;
        if (unsaved) alertUser(DISCARDED, 'warning');
      }
      this.loadFailed = false;
    } catch (error) {
      if (token !== this.loadToken) return null;
      if ((error as { status?: number }).status === 404) {
        // There is no Rough Cut (any more), which is not an error to shout about.
        this.body = null;
        this.clips = [];
        this.captionDraft = null;
        this.loadFailed = false;
      } else {
        // Any other failure says nothing about the timeline: keep the one on screen (and its unsaved edits).
        failure = errorMessage(error);
        this.loadFailed = true;
      }
      log.warn('could not load the Edit Timeline', error);
    } finally {
      if (token === this.loadToken) this.loaded = true;
    }
    return failure;
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
      this.announce = '';
    }
    this.requestUpdate();
  }

  /** Refuses an edit: shows why, and (re-render) puts the last accepted value back into the field. */
  private refuse(clip: EditClip, problem: string): void {
    alertUser(`${clip.label}: ${problem}`);
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
    if (from >= 0) this.moveTo(from, to);
  }

  /** Saves the clips as the next version. */
  async save(): Promise<void> {
    const body = this.body;
    if (!body || this.locked) return;
    this.busy = true;
    try {
      await put(body.save_url, toPayload(this.clips));
      this.busy = false;
      const failure = await this.fetchLatest();
      if (failure !== null) {
        // Saved, but the screen still shows the old version (unsaved changes, a save from it would be refused as stale): not "updated".
        alertUser(
          `保存は完了しましたが、最新の版の再読込に失敗しました。「変更を戻す」で再読込してください（${failure}）`,
        );
        return;
      }
      showToast('保存しました');
      this.dispatchEvent(
        new CustomEvent(TIMELINE_UPDATED_EVENT, { detail: { id: this.body?.id ?? '' }, bubbles: true, composed: true }),
      );
    } catch (error) {
      alertUser(errorMessage(error));
    } finally {
      this.busy = false;
      focusToolbar(this);
    }
  }

  /** "変更を戻す": drops the unsaved edits for the latest version, which cannot be taken back, so it is asked first (W10). */
  private async revert(): Promise<void> {
    if (!(await confirmDialog(REVERT_CONFIRM))) return;
    await this.load();
    focusToolbar(this);
  }

  private moveTo(from: number, to: number): void {
    const clip = this.clips[from];
    this.change(moveClip(this.clips, from, to));
    if (!clip) return;
    this.announce = `${clip.label} を ${to + 1} 番目へ移動しました`;
    focusMoved(this, clip.shot_id, from, to);
  }

  private removeAt(at: number): void {
    const clip = this.clips[at];
    this.change(removeClip(this.clips, at));
    if (!clip) return;
    this.announce = `${clip.label} を外しました（${this.clips.length} クリップ）`;
    focusRemoved(this, this.clips[Math.min(at, this.clips.length - 1)]?.shot_id);
  }

  protected override render() {
    if (!this.loaded) return html`<p class="text-sm text-mute" role="status">読み込み中…</p>`;
    const body = this.body;
    if (!body) {
      // A failed first load is an alert in #alert-region and nothing here; only a 404 means "no Rough Cut yet".
      if (this.loadFailed) return nothing;
      return html`<p class="rounded-lg border border-dashed border-border p-6 text-sm text-mute" data-empty>
          Rough Cut がまだありません。各 Shot の採用 Take を選んでから「Rough Cut を作成」を押してください。
        </p>`;
    }
    return html`
      <div class="flex flex-col gap-4">
        <p class="sr-only" role="status" data-announce>${this.announce}</p>
        <mam-video-player video-src=${body.preview_url}></mam-video-player>
        <ol class="flex flex-col gap-2" data-clips>
          ${repeat(this.clips, clipKey, (c, i) => this.renderClip(c, i, body))}
        </ol>
        ${renderToolbar({
          clips: this.clips,
          saved: body.clips,
          busy: this.busy,
          readonly: body.readonly,
          save: () => void this.save(),
          revert: () => void this.revert(),
        })}
      </div>
    `;
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
      move: (from, to) => this.moveTo(from, to),
      remove: (at) => this.removeAt(at),
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
