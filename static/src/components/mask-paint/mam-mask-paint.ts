import { LitElement, html } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { confirmDialog } from '../confirm/confirm';
import { sliderStyle } from '../slider/slider-model';
import { meanLuminance, overlayToken, overlayWhite, strokeLine, type Point } from './mask-draw';
import { canvasSize } from './mask-size';

/** A small sub button of the guide (W3): 32px high, 14px text, a ripple on press (`data-ripple` in the markup). */
const BUTTON =
  'inline-flex min-h-8 items-center justify-center rounded-md border px-3 py-1 text-sm font-semibold text-ink transition-colors hover:bg-accent-soft ' +
  'focus-visible:focus-ring motion-reduce:transition-none';
const PLAIN = 'border-border';
const SELECTED = 'border-accent bg-accent-soft';
/** The question before the mask is wiped: it cannot be taken back (W10). */
const CLEAR_CONFIRM = '塗った部分をすべて消しますか？';
const MAX_SIZE = 128;

/**
 * `<mam-mask-paint>` paints the part of a picture to redraw (spec/components/gentest-screen.md, #974).
 *
 * The mask is a black canvas (keep) with white strokes (redraw). The view shows those strokes in red over
 * the source image, so black is not drawn as red. `maskPNG()` exports the black/white PNG. When htmx is about to
 * send the surrounding form (`htmx:confirm`), the request is held until the PNG is in `input[type=file][name=mask]`,
 * then released: the form is sent once, with the mask (the submit itself is never cancelled and re-sent, which htmx
 * would answer twice).
 *
 * Memory (#999): the two canvases are the picture's size, or scaled down to `MAX_EDGE` on the longer side (mask-size.ts) when the
 * picture is bigger; the server and ComfyUI take a mask of another size (the nodes stretch it to the picture). The picture itself is
 * shown as a copy of the canvases' size (a third canvas under the view), not as an `<img>`: Chrome kept an `<img>` of an 8192 × 8192
 * picture decoded at its own size (about 256 MiB more for the tab), the copy is 16 MiB. `mask-src` is a mask to start from (the
 * black/white picture of an earlier redraw, "この設定でもう一度"): it is drawn on the canvases once the picture is.
 *
 * What is painted shows as a red overlay in the colour of the `--overlay-deep` token over a bright picture and `--overlay-pale` over a dark one (the
 * picture's mean luminance decides, read when it is shown; the two keep 3:1 against the picture where they apply, color-rules.md §4.2; the exported
 * mask is data, black and white, not a colour of the screen). Without a picture there is no canvas, only an empty state (lit-widgets-guide §5).
 *
 * A drag draws a round-capped line from the previous position, so a fast drag leaves no gaps.
 * Keyboard: B brush, E eraser, [ ] thickness, arrows draw a line one thickness long from the cursor (kept inside
 * the picture). The canvas is in the tab order. It renders into the light DOM so the page's Tailwind classes apply.
 */
@customElement('mam-mask-paint')
export class MamMaskPaint extends LitElement {
  @property({ type: String }) src = '';
  @property({ type: String, attribute: 'source-input' }) sourceInput = '';
  @property({ type: String, attribute: 'mask-src' }) maskSrc = '';
  @property({ type: Number }) width = 64;
  @property({ type: Number }) height = 64;

  @state() private tool: 'brush' | 'eraser' = 'brush';
  @state() private size = 16;

  private view?: HTMLCanvasElement;
  private picture?: HTMLCanvasElement;
  private mask?: HTMLCanvasElement;
  private form: HTMLFormElement | null = null;
  private objectURL = '';
  private last: Point | null = null;
  private cursor: Point = { x: 32, y: 32 };
  private builtFor = '';
  /** The token of the overlay colour: it follows the brightness of the picture. */
  private token = overlayToken(null);

  protected override createRenderRoot(): HTMLElement {
    return this;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.form = this.closest('form');
    this.form?.addEventListener('htmx:confirm', this.onConfirm);
    const input = this.sourceInput ? document.getElementById(this.sourceInput) : null;
    input?.addEventListener('change', this.onFile);
  }

  override disconnectedCallback(): void {
    this.form?.removeEventListener('htmx:confirm', this.onConfirm);
    document.getElementById(this.sourceInput)?.removeEventListener('change', this.onFile);
    if (this.objectURL) URL.revokeObjectURL(this.objectURL);
    super.disconnectedCallback();
  }

  protected override updated(): void {
    // The view is the first canvas; it is positioned, so it is painted over the picture that follows it.
    this.view = this.renderRoot.querySelector('canvas') ?? undefined;
    this.picture = this.renderRoot.querySelector<HTMLCanvasElement>('canvas[data-picture]') ?? undefined;
    if (!this.mask) this.mask = document.createElement('canvas');
    if (this.src && this.builtFor !== this.src) this.load(this.src);
    else if (!this.src && this.builtFor !== 'blank') this.resize(this.width, this.height, 'blank');
  }

  /** The black/white PNG (white redraws, black keeps). */
  maskPNG(): Promise<Blob> {
    const canvas = this.mask;
    if (!canvas) return Promise.reject(new Error('mask'));
    return new Promise((resolve, reject) => {
      canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('mask'))), 'image/png');
    });
  }

  private load(src: string): void {
    const img = new Image();
    img.onload = () => {
      if (this.src !== src) return;
      const size = canvasSize(img.naturalWidth || this.width, img.naturalHeight || this.height);
      this.resize(size.width, size.height, src);
      this.showPicture(img);
      this.applyMask(src);
    };
    img.src = src;
  }

  /** Draws the picture, scaled to the canvases' size, on the canvas under the view. */
  private showPicture(img: HTMLImageElement): void {
    if (!this.picture || !this.view) return;
    this.picture.width = this.view.width;
    this.picture.height = this.view.height;
    const g = this.picture.getContext('2d');
    if (!g) return;
    g.imageSmoothingQuality = 'high';
    g.drawImage(img, 0, 0, this.picture.width, this.picture.height);
    this.token = overlayToken(meanLuminance(this.picture));
  }

  /** Draws the mask to start from on the canvases: white (what the picture says is redrawn) becomes red on the view. */
  private applyMask(src: string): void {
    if (!this.maskSrc) return;
    const img = new Image();
    img.onload = () => {
      if (this.src !== src || !this.view || !this.mask) return;
      const { width: w, height: h } = this.mask;
      const g = this.mask.getContext('2d');
      const v = this.view.getContext('2d');
      if (!g || !v) return;
      g.drawImage(img, 0, 0, w, h);
      overlayWhite(v, g, this.overlayColor());
    };
    img.src = this.maskSrc;
  }

  private resize(w: number, h: number, key: string): void {
    if (!this.mask) return;
    if (this.view) {
      this.view.width = w;
      this.view.height = h;
    }
    this.mask.width = w;
    this.mask.height = h;
    const g = this.mask.getContext('2d');
    if (!g) return;
    g.fillStyle = '#000';
    g.fillRect(0, 0, w, h);
    this.view?.getContext('2d')?.clearRect(0, 0, w, h);
    this.cursor = { x: Math.floor(w / 2), y: Math.floor(h / 2) };
    this.builtFor = key;
  }

  private onFile = (): void => {
    const input = document.getElementById(this.sourceInput) as HTMLInputElement | null;
    const file = input?.files?.[0];
    if (!file) return;
    if (this.objectURL) URL.revokeObjectURL(this.objectURL);
    this.objectURL = URL.createObjectURL(file);
    this.src = this.objectURL;
  };

  private setTool(tool: 'brush' | 'eraser'): void {
    this.tool = tool;
  }

  private setSize(n: number): void {
    this.size = Math.min(MAX_SIZE, Math.max(1, Math.round(n)));
  }

  /** Wipes the mask once the user confirmed: the painting cannot be brought back. */
  private askClear(): void {
    void confirmDialog(CLEAR_CONFIRM).then((yes) => {
      if (yes) this.clear();
    });
  }

  private clear(): void {
    if (!this.view || !this.mask) return;
    this.resize(this.view.width, this.view.height, this.builtFor);
  }

  /** The colour of the overlay: the token that suits the picture (`--overlay-deep` or `--overlay-pale`). */
  private overlayColor(): string {
    return getComputedStyle(this).getPropertyValue(this.token).trim();
  }

  private pos(e: PointerEvent): Point {
    const c = this.view!;
    const r = c.getBoundingClientRect();
    const sx = r.width > 0 ? c.width / r.width : 1;
    const sy = r.height > 0 ? c.height / r.height : 1;
    return { x: (e.clientX - r.left) * sx, y: (e.clientY - r.top) * sy };
  }

  private down = (e: PointerEvent): void => {
    try {
      (e.currentTarget as HTMLCanvasElement).setPointerCapture(e.pointerId);
    } catch {
      // The pointer is gone (or was never a live one): the stroke is still drawn, it just ends when the pointer leaves the canvas.
    }
    const at = this.pos(e);
    this.last = at;
    this.line(at, at);
  };

  private move = (e: PointerEvent): void => {
    if (!this.last) return;
    const at = this.pos(e);
    this.line(this.last, at);
    this.last = at;
  };

  private up = (): void => {
    this.last = null;
  };

  /** Draws the stroke from `from` to `to` (a dab when they are the same) on the mask and on the view. */
  private line(from: Point, to: Point): void {
    if (!this.view || !this.mask) return;
    const brush = this.tool === 'brush';
    strokeLine(this.mask, from, to, this.size, brush ? '#fff' : '#000', 'source-over');
    strokeLine(this.view, from, to, this.size, this.overlayColor(), brush ? 'source-over' : 'destination-out');
    this.cursor = to;
  }

  private step(dx: number, dy: number): void {
    const from = this.cursor;
    const w = this.view?.width ?? this.width;
    const h = this.view?.height ?? this.height;
    const n = Math.max(1, this.size);
    const to = {
      x: Math.min(w, Math.max(0, from.x + dx * n)),
      y: Math.min(h, Math.max(0, from.y + dy * n)),
    };
    this.line(from, to);
  }

  private key = (e: KeyboardEvent): void => {
    if (e.key === 'b' || e.key === 'B') this.setTool('brush');
    else if (e.key === 'e' || e.key === 'E') this.setTool('eraser');
    else if (e.key === '[') this.setSize(this.size - 2);
    else if (e.key === ']') this.setSize(this.size + 2);
    else if (e.key === 'ArrowLeft') this.step(-1, 0);
    else if (e.key === 'ArrowRight') this.step(1, 0);
    else if (e.key === 'ArrowUp') this.step(0, -1);
    else if (e.key === 'ArrowDown') this.step(0, 1);
    else return;
    e.preventDefault();
  };

  /** Holds htmx's request of the form until the mask is in the file input, then releases it (once). */
  private onConfirm = (e: Event): void => {
    const form = this.form;
    const detail = (e as CustomEvent<{ elt: Element; issueRequest: () => void }>).detail;
    if (!form || detail.elt !== form) return;
    e.preventDefault();
    const issue = detail.issueRequest;
    void this.maskPNG()
      .then((blob) => {
        const input = form.querySelector<HTMLInputElement>('input[type="file"][name="mask"]');
        if (!input) return;
        const dt = new DataTransfer();
        dt.items.add(new File([blob], 'mask.png', { type: 'image/png' }));
        input.files = dt.files;
      })
      .catch(() => undefined)
      .then(() => issue());
  };

  private toolButton(label: string, tool: 'brush' | 'eraser', key: string) {
    const on = this.tool === tool;
    return html`<button type="button" class="${BUTTON} ${on ? SELECTED : PLAIN}" aria-pressed=${String(on)} aria-keyshortcuts=${key} data-ripple @click=${() => this.setTool(tool)}>${label}</button>`;
  }

  protected override render() {
    return html`
      <div class="flex flex-col gap-2" data-testid="gt-mask-paint">
        <div role="group" aria-label="描き方" class="flex flex-wrap items-center gap-2">
          ${this.toolButton('筆', 'brush', 'B')} ${this.toolButton('消しゴム', 'eraser', 'E')}
          <button type="button" class="${BUTTON} ${PLAIN} disabled:cursor-not-allowed disabled:opacity-40" data-ripple ?disabled=${!this.src} @click=${() => this.askClear()}>全部消す</button>
        </div>
        <label class="flex items-center gap-2 text-xs text-mute">
          太さ
          <input
            type="range"
            min="1"
            max=${MAX_SIZE}
            style=${sliderStyle({ value: String(this.size), min: '1', max: String(MAX_SIZE) })}
            .value=${String(this.size)}
            aria-label="太さ"
            @input=${(e: Event) => this.setSize(Number((e.target as HTMLInputElement).value))}
          />
          <output class="inline-block w-8 text-right font-mono tabular-nums text-ink" data-size>${this.size}</output>
        </label>
        ${
          this.src
            ? html`<div class="relative inline-block max-w-full self-start has-[canvas:focus-visible]:focus-ring" data-stage>
              <canvas
                tabindex="0"
                aria-label="描き直す部分。白く塗ったところが描き直されます"
                class="absolute inset-0 h-full w-full focus-visible:outline-none"
                style="touch-action: none; cursor: crosshair; opacity: 0.55"
                @pointerdown=${this.down}
                @pointermove=${this.move}
                @pointerup=${this.up}
                @pointercancel=${this.up}
                @keydown=${this.key}
              ></canvas>
              <canvas data-picture aria-hidden="true" class="block max-w-full"></canvas>
            </div>`
            : html`<p class="rounded-lg border border-dashed border-border p-6 text-sm text-mute" data-empty>画像を選ぶと、描き直す部分を塗れます</p>`
        }
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'mam-mask-paint': MamMaskPaint;
  }
}
