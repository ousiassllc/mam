import { LitElement, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { ApiError, get } from '../lib/api';
import { createLogger } from '../lib/logger';
import { isFinished, type ShotProgress } from '../lib/shot';
import { DEFAULT_FPS, seekToFrame, stepFrame, timeToFrame, type JudgeIssue } from '../lib/video';
import { createConnectionNotice } from '../lib/connection-notice';
import { connect, socketUrl, type Connection } from '../lib/ws';
import { probeFrameRate, sampleFrameRate } from './video-fps';
import { buildMarkers, renderPlaceholder, renderTransport } from './video-parts';

const log = createLogger('video-player');

interface JudgeResultBody {
  issues?: JudgeIssue[];
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
  private probing: Promise<void> | null = null;
  @state() private currentTime = 0;
  @state() private duration = 0;
  @state() private playing = false;
  @state() private progress: ShotProgress | null = null;
  @state() private failed = false;

  private connection: Connection | null = null;
  private readonly notice = createConnectionNotice('生成の進捗');
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
    this.notice.hide();
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
    this.notice.hide();
    if (!this.wsUrl) return;
    this.connection = connect<ShotProgress>({
      url: socketUrl(this.wsUrl, location),
      onOpen: () => this.notice.hide(),
      onGiveUp: () => this.notice.show(() => this.connection?.retry()),
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
    if (!this.frameRateKnown) await this.probe(video);
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

  /** Plays the video muted for a moment to measure its frame rate (see video-fps). Callers during a probe share it: a second probe would take the state the first one made (muted, advanced time) for the original and restore that. */
  private probe(video: HTMLVideoElement): Promise<void> {
    this.probing ??= probeFrameRate(video, () => this.frameRateKnown).finally(() => (this.probing = null));
    return this.probing;
  }

  private onScrub(event: Event): void {
    const video = this.video;
    if (!video) return;
    const value = Number((event.target as HTMLInputElement).value);
    this.seek(value);
  }

  private onKey(event: KeyboardEvent): void {
    // Alt+← / Cmd+← / Ctrl+← … are the browser's (back, forward, word jump): leave them alone. Shift alone steps ten frames.
    if (event.altKey || event.ctrlKey || event.metaKey) return;
    const keys: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1 };
    const delta = keys[event.key];
    if (delta === undefined) return;
    event.preventDefault();
    void this.step(event.shiftKey ? delta * 10 : delta);
  }

  protected override render() {
    return this.renderBody();
  }

  private renderBody() {
    const stage = this.progress?.stage;
    const generating = stage === 'queued' || stage === 'generating' || stage === 'repairing';
    if (!this.videoSrc || generating) return renderPlaceholder(this.progress);
    if (this.failed) {
      return html`<div class="flex aspect-video items-center justify-center rounded border border-neutral-700 bg-neutral-900 text-xs text-neutral-400" role="alert" data-placeholder>
        動画を再生できません
      </div>`;
    }
    const max = this.duration || 0;
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
            if (!this.frameRateKnown)
              sampleFrameRate(e.target as HTMLVideoElement, (rate) => (this.measuredFps = rate));
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
        ${renderTransport({
          max,
          rate: this.frameRate,
          rateKnown: this.frameRateKnown,
          currentTime: this.currentTime,
          playing: this.playing,
          markers: buildMarkers(this.issues, this.frameRateKnown ? this.frameRate : 0),
          scrub: (e) => this.onScrub(e),
          step: (delta) => void this.step(delta),
          toggle: () => this.togglePlay(),
          seek: (time) => this.seek(time),
        })}
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'mam-video-player': MamVideoPlayer;
  }
}
