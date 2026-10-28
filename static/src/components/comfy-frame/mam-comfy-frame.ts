import { LitElement, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

/** How long the ComfyUI page may take to finish loading before the fallback is shown. */
export const DEFAULT_TIMEOUT_MS = 20_000;

export type FrameState = 'loading' | 'loaded' | 'failed';

/**
 * `<mam-comfy-frame>`: the ComfyUI node screen in an iframe (spec/architecture/comfy/comfy-embedding.md).
 *
 * - `src`: the ComfyUI address, built by the server with the same literal host as this page (ComfyUI refuses
 *   requests the browser calls cross-site; 127.0.0.1 and localhost are different sites).
 * - `open-url`: the same address for the "ブラウザで開く" link, which is always there.
 * - `failure-message`: what to say when the page did not load; with the link the user can still get to ComfyUI.
 * - `timeout-ms`: the wait for the iframe's `load` event (default 20 s).
 *
 * The browser does not tell this page why a cross-origin frame stayed empty (the HTTP status of the frame is
 * hidden), so the component cannot tell "refused" from "slow": a frame that fired `error` or did not fire
 * `load` in time is shown as failed, with the message and the link. A refusal that answers with a page (an
 * HTTP 403) does fire `load`; the server finds that case beforehand (workflows.Service.NodeTarget) and does
 * not draw the frame. It fires `comfy-frame-failed` (bubbles, composed) once when it gives up.
 * It renders into the light DOM so that the page's Tailwind classes apply.
 */
@customElement('mam-comfy-frame')
export class MamComfyFrame extends LitElement {
  @property({ type: String }) src = '';
  @property({ type: String, attribute: 'open-url' }) openUrl = '';
  @property({ type: String, attribute: 'failure-message' }) failureMessage = '';
  @property({ type: String, attribute: 'open-label' }) openLabel = 'ブラウザで開く';
  @property({ type: Number, attribute: 'timeout-ms' }) timeoutMs = DEFAULT_TIMEOUT_MS;

  @state() private status: FrameState = 'loading';
  private timer: ReturnType<typeof setTimeout> | undefined;

  protected override createRenderRoot(): HTMLElement {
    return this;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.arm();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.disarm();
  }

  protected override willUpdate(changed: Map<string, unknown>): void {
    if (changed.has('src') && this.hasUpdated) {
      this.status = 'loading';
      this.arm();
    }
  }

  /** The state the frame is in. */
  get frameState(): FrameState {
    return this.status;
  }

  private arm(): void {
    this.disarm();
    if (!this.src) return;
    this.timer = setTimeout(() => this.fail(), this.timeoutMs);
  }

  private disarm(): void {
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
  }

  private loaded(): void {
    if (this.status === 'failed') return;
    this.disarm();
    this.status = 'loaded';
  }

  private fail(): void {
    if (this.status !== 'loading') return;
    this.disarm();
    this.status = 'failed';
    this.dispatchEvent(new CustomEvent('comfy-frame-failed', { bubbles: true, composed: true }));
  }

  /** "ブラウザで開く": an external ComfyUI the iframe cannot show can still be used on its own. */
  private link() {
    if (!this.openUrl) return nothing;
    return html`<a
      data-testid="comfy-frame-open"
      class="text-sm text-neutral-300 underline hover:text-neutral-100"
      href=${this.openUrl}
      target="_blank"
      rel="noopener noreferrer"
      >${this.openLabel}</a
    >`;
  }

  protected override render() {
    const failed = this.status === 'failed';
    return html`
      <div class="flex flex-col gap-2">
        ${
          failed
            ? html`<div
              role="alert"
              data-testid="comfy-frame-failure"
              class="rounded-md border border-amber-500/60 p-3 text-sm text-amber-300"
            >
              ${this.failureMessage || 'ComfyUI の画面を表示できませんでした。'}
            </div>`
            : nothing
        }
        ${
          this.src && !failed
            ? html`<iframe
              data-testid="comfy-frame"
              class="h-[75vh] w-full rounded-md border border-neutral-800 bg-neutral-900"
              title="ComfyUI ノード画面"
              src=${this.src}
              allow="clipboard-read; clipboard-write; fullscreen"
              referrerpolicy="no-referrer"
              @load=${() => this.loaded()}
              @error=${() => this.fail()}
            ></iframe>`
            : nothing
        }
        <div class="flex items-center gap-3">
          ${this.link()}
          ${
            this.status === 'loading' && this.src
              ? html`<span class="text-xs text-neutral-400" data-testid="comfy-frame-loading">読み込んでいます…</span>`
              : nothing
          }
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'mam-comfy-frame': MamComfyFrame;
  }
}
