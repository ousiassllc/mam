import { LitElement, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { ApiError, get } from '../lib/api';
import { createLogger } from '../lib/logger';
import { describeProgress, isFinished, progressFraction, type ShotProgress } from '../lib/shot';
import {
  DEFAULT_FPS,
  estimateFps,
  formatTimecode,
  issueRange,
  seekToFrame,
  stepFrame,
  timeToFrame,
  type JudgeIssue,
  type TimeRange,
} from '../lib/video';
import { connect, socketUrl, type Connection } from '../lib/ws';

const log = createLogger('video-player');

/** Number of presentation times read to measure the frame rate of a file. */
const FPS_SAMPLES = 8;
/** How long the muted probe may run to measure the frame rate before it gives up. */
const FPS_PROBE_MS = 1500;

interface JudgeResultBody {
  issues?: JudgeIssue[];
}

/** An Issue as the time bar draws it. */
interface Marker {
  issue: JudgeIssue;
  range: TimeRange;
}

export interface SeekRequestedDetail {
  /** Seconds. */
  time: number;
  /** 0-based number of the frame at `time`. */
  frame: number;
}

/**
 * `<mam-video-player>`: plays a Take with frame-accurate seeking (spec/components/overview.md §4.1).
 *
 * - `video-src`: the file to play. It is a same-origin URL the server resolved (a 1080p proxy when the
 *   file is 4K), so the player never builds one.
 * - `api-url`: GET returns the Judge result of the Take; its Issues are drawn as markers on the time
 *   bar (a 404 means the Take has not been judged: no markers).
 * - `ws-url`: /ws/shots/:id. While the Shot is being generated the placeholder shows the progress.
 * - `fps`: the frame rate when the server knows it; otherwise it is measured while the video plays.
 *
 * It fires `seek-requested` (bubbles, composed) whenever the user moves the play position.
 * It renders into the light DOM so that the page's Tailwind classes apply.
 */
@customElement('mam-video-player')
export class MamVideoPlayer extends LitElement {
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';
  @property({ type: String, attribute: 'video-src' }) videoSrc = '';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';
  @property({ type: String }) poster = '';
  @property({ type: Number }) fps = 0;

  @state() private issues: JudgeIssue[] = [];
  @state() private measuredFps = 0;
  @state() private currentTime = 0;
  @state() private duration = 0;
  @state() private playing = false;
  @state() private progress: ShotProgress | null = null;
  @state() private failed = false;

  private connection: Connection | null = null;
  private loadToken = 0;

  protected override createRenderRoot(): HTMLElement {
    return this;
  }

  /** Frame rate used for stepping and timecodes: the given one, the measured one, else 30. */
  get frameRate(): number {
    return this.fps > 0 ? this.fps : this.measuredFps > 0 ? this.measuredFps : DEFAULT_FPS;
  }

  /** True when the frame rate is the file's own and not a guess. */
  get frameRateKnown(): boolean {
    return this.fps > 0 || this.measuredFps > 0;
  }

  private get video(): HTMLVideoElement | null {
    return this.querySelector('video');
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.connection?.close();
    this.connection = null;
    this.loadToken += 1;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    // Coming back after a disconnect (moved in the DOM): the properties did not change, so reload.
    if (this.hasUpdated) {
      void this.loadIssues();
      this.watchProgress();
    }
  }

  protected override updated(changed: Map<string, unknown>): void {
    if (changed.has('apiUrl')) void this.loadIssues();
    if (changed.has('wsUrl')) this.watchProgress();
  }

  protected override willUpdate(changed: Map<string, unknown>): void {
    // A new file: forget the position of the old one (in willUpdate, so that no second update is scheduled).
    if (changed.has('videoSrc') && changed.get('videoSrc') !== undefined) {
      this.currentTime = 0;
      this.duration = 0;
      this.playing = false;
      this.failed = false;
    }
  }

  private async loadIssues(): Promise<void> {
    const token = ++this.loadToken;
    if (!this.apiUrl) {
      if (this.issues.length > 0) this.issues = [];
      return;
    }
    try {
      const body = await get<JudgeResultBody>(this.apiUrl);
      if (token === this.loadToken) this.issues = body.issues ?? [];
    } catch (error) {
      if (token !== this.loadToken) return;
      this.issues = [];
      if (!(error instanceof ApiError && error.status === 404)) log.warn('could not load the Judge result', error);
    }
  }

  private watchProgress(): void {
    this.connection?.close();
    this.connection = null;
    this.progress = null;
    if (!this.wsUrl) return;
    this.connection = connect<ShotProgress>({
      url: socketUrl(this.wsUrl, location),
      onMessage: (progress) => {
        this.progress = progress;
        if (isFinished(progress)) {
          this.connection?.close();
          this.connection = null;
          this.dispatchEvent(new CustomEvent('progress-finished', { detail: progress, bubbles: true, composed: true }));
        }
      },
    });
  }

  /** Seeks to a time in seconds (snapped to a frame) and tells the others. */
  seek(time: number): void {
    this.seekFrame(timeToFrame(time, this.frameRate));
  }

  /** Seeks to a frame number and tells the others. */
  seekFrame(frame: number): void {
    const video = this.video;
    if (!video) return;
    const time = seekToFrame(video, frame, this.frameRate);
    this.currentTime = time;
    this.playing = false;
    this.dispatchEvent(
      new CustomEvent<SeekRequestedDetail>('seek-requested', {
        detail: { time, frame: timeToFrame(time, this.frameRate) },
        bubbles: true,
        composed: true,
      }),
    );
  }

  /** Moves by `delta` frames (negative: back). */
  async step(delta: number): Promise<void> {
    const video = this.video;
    if (!video) return;
    if (!this.frameRateKnown) await this.probeFrameRate(video);
    const frame = stepFrame(video.currentTime, delta, this.frameRate, this.duration || video.duration || 0);
    this.seekFrame(frame);
  }

  private togglePlay(): void {
    const video = this.video;
    if (!video) return;
    if (video.paused) {
      void video.play().catch((error: unknown) => log.warn('play failed', error));
    } else {
      video.pause();
    }
  }

  /** Reads the presentation times of the next frames while the video runs. */
  private sampleFrames(video: HTMLVideoElement): void {
    if (this.frameRateKnown || typeof video.requestVideoFrameCallback !== 'function') return;
    const times: number[] = [];
    const onFrame = (_now: number, meta: VideoFrameCallbackMetadata) => {
      times.push(meta.mediaTime);
      if (times.length >= FPS_SAMPLES) {
        const rate = estimateFps(times);
        if (rate > 0) this.measuredFps = rate;
        return;
      }
      if (!video.paused) video.requestVideoFrameCallback(onFrame);
    };
    video.requestVideoFrameCallback(onFrame);
  }

  /** Plays the video muted for a moment to measure its frame rate, then puts it back where it was. */
  private async probeFrameRate(video: HTMLVideoElement): Promise<void> {
    if (typeof video.requestVideoFrameCallback !== 'function') return;
    const time = video.currentTime;
    const muted = video.muted;
    const wasPaused = video.paused;
    video.muted = true;
    try {
      await video.play();
      const deadline = Date.now() + FPS_PROBE_MS;
      while (!this.frameRateKnown && Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, 30));
      }
    } catch (error) {
      log.warn('could not measure the frame rate', error);
    } finally {
      if (wasPaused) video.pause();
      video.currentTime = time;
      video.muted = muted;
    }
  }

  private markers(): Marker[] {
    const out: Marker[] = [];
    const rate = this.frameRateKnown ? this.frameRate : 0;
    for (const issue of this.issues) {
      const range = issueRange(issue, rate);
      if (range) out.push({ issue, range });
    }
    return out;
  }

  private onScrub(event: Event): void {
    const video = this.video;
    if (!video) return;
    const value = Number((event.target as HTMLInputElement).value);
    this.seek(value);
  }

  private onKey(event: KeyboardEvent): void {
    const keys: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1 };
    const delta = keys[event.key];
    if (delta === undefined) return;
    event.preventDefault();
    void this.step(event.shiftKey ? delta * 10 : delta);
  }

  protected override render() {
    const generating = this.progress?.stage === 'queued' || this.progress?.stage === 'generating';
    if (!this.videoSrc || generating) return this.renderPlaceholder();
    if (this.failed) {
      return html`<div class="flex aspect-video items-center justify-center rounded border border-neutral-700 bg-neutral-900 text-xs text-neutral-400" role="alert" data-placeholder>
        動画を再生できません
      </div>`;
    }
    const max = this.duration || 0;
    const markers = this.markers();
    return html`
      <div class="flex flex-col gap-1" @keydown=${this.onKey}>
        <video
          class="max-h-80 w-full rounded border border-neutral-700 bg-black"
          src=${this.videoSrc}
          poster=${this.poster || nothing}
          preload="metadata"
          playsinline
          @loadedmetadata=${(e: Event) => {
            this.duration = (e.target as HTMLVideoElement).duration;
          }}
          @timeupdate=${(e: Event) => {
            this.currentTime = (e.target as HTMLVideoElement).currentTime;
          }}
          @play=${(e: Event) => {
            this.playing = true;
            this.sampleFrames(e.target as HTMLVideoElement);
          }}
          @pause=${() => {
            this.playing = false;
          }}
          @ended=${() => {
            this.playing = false;
          }}
          @error=${() => {
            this.failed = true;
          }}
        ></video>
        <div class="relative" data-timebar>
          <input
            type="range"
            class="w-full"
            aria-label="再生位置"
            min="0"
            max=${max}
            step=${1 / this.frameRate}
            .value=${String(this.currentTime)}
            ?disabled=${max === 0}
            @input=${this.onScrub}
          />
          ${max > 0
            ? html`<div class="pointer-events-none absolute inset-x-0 top-0 h-full">
                ${markers.map((m) => this.renderMarker(m, max))}
              </div>`
            : nothing}
        </div>
        <div class="flex items-center gap-2 text-xs text-neutral-300">
          <button type="button" data-action="prev-frame" class="rounded border border-neutral-700 px-2 py-0.5" aria-label="1 コマ戻る" @click=${() => void this.step(-1)}>◀</button>
          <button type="button" data-action="play" class="rounded border border-neutral-700 px-2 py-0.5" aria-label=${this.playing ? '一時停止' : '再生'} @click=${this.togglePlay}>
            ${this.playing ? '❚❚' : '▶'}
          </button>
          <button type="button" data-action="next-frame" class="rounded border border-neutral-700 px-2 py-0.5" aria-label="1 コマ進む" @click=${() => void this.step(1)}>▶</button>
          <span class="font-mono" data-timecode>${formatTimecode(this.currentTime, this.frameRate)}</span>
          <span class="font-mono text-neutral-500">/ ${formatTimecode(max, this.frameRate)}</span>
          <span class="text-neutral-500">${this.frameRate.toFixed(this.frameRate % 1 === 0 ? 0 : 2)} fps${this.frameRateKnown ? '' : '（仮）'}</span>
        </div>
      </div>
    `;
  }

  private renderMarker(marker: Marker, max: number) {
    const { range, issue } = marker;
    const left = Math.min(range.start / max, 1) * 100;
    const width = Math.max(Math.min((range.end - range.start) / max, 1 - left / 100) * 100, 0.8);
    const color = issue.severity === 'high' ? 'bg-red-500/70' : issue.severity === 'medium' ? 'bg-amber-500/70' : 'bg-sky-500/70';
    const label = `${issue.type}（${issue.severity}） ${range.start.toFixed(1)}-${range.end.toFixed(1)}秒`;
    return html`<button
      type="button"
      data-marker
      data-severity=${issue.severity}
      class="pointer-events-auto absolute top-0 h-full rounded-sm ${color}"
      style="left:${left}%;width:${width}%"
      title=${label}
      aria-label=${label}
      @click=${() => this.seek(range.start)}
    ></button>`;
  }

  private renderPlaceholder() {
    const fraction = progressFraction(this.progress);
    return html`<div
      class="flex aspect-video flex-col items-center justify-center gap-2 rounded border border-dashed border-neutral-700 bg-neutral-900 text-xs text-neutral-400"
      role="status"
      data-placeholder
      data-stage=${this.progress?.stage ?? 'idle'}
    >
      <span>${describeProgress(this.progress)}</span>
      ${fraction === null
        ? nothing
        : html`<div class="h-1 w-1/2 overflow-hidden rounded bg-neutral-800">
            <div class="h-full bg-neutral-300" style="width:${fraction * 100}%" data-progress-bar></div>
          </div>`}
    </div>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'mam-video-player': MamVideoPlayer;
  }
}
