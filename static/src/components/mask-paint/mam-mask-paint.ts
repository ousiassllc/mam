import { LitElement, html } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { sliderStyle } from '../slider/slider-model';
import { canvasSize } from './mask-size';

type Point = { x: number; y: number };

const BUTTON =
  'inline-flex items-center justify-center rounded-md border border-border px-2 py-1 text-xs font-semibold text-ink transition-colors hover:bg-accent-soft ' +
  'focus-visible:focus-ring';
const SELECTED = 'border-accent bg-accent-soft';
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
 * picture is bigger; the server and ComfyUI take a mask of another size (the nodes stretch it to the picture). `mask-src` is a mask to
 * start from (the black/white picture of an earlier redraw, "この設定でもう一度"): it is drawn on the canvases once the picture is.
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
  private mask?: HTMLCanvasElement;
  private form: HTMLFormElement | null = null;
  private objectURL = '';
  private last: Point | null = null;
  private cursor: Point = { x: 32, y: 32 };
  private builtFor = '';

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
    this.view = this.renderRoot.querySelector('canvas') ?? undefined;
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
      this.applyMask(src);
    };
    img.src = src;
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
      const mask = g.getImageData(0, 0, w, h).data;
      const red = v.createImageData(w, h);
      for (let i = 0; i < mask.length; i += 4) {
        red.data[i] = 0xff;
        red.data[i + 1] = 0x33;
        red.data[i + 2] = 0x33;
        red.data[i + 3] = mask[i] ?? 0;
      }
      v.putImageData(red, 0, 0);
    };
    img.src = this.maskSrc;
  }

  private resize(w: number, h: number, key: string): void {
    if (!this.view || !this.mask) return;
    this.view.width = w;
    this.view.height = h;
    this.mask.width = w;
    this.mask.height = h;
    const g = this.mask.getContext('2d');
    if (!g) return;
    g.fillStyle = '#000';
    g.fillRect(0, 0, w, h);
    this.view.getContext('2d')?.clearRect(0, 0, w, h);
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

  private clear(): void {
    if (!this.view || !this.mask) return;
    this.resize(this.view.width, this.view.height, this.builtFor);
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
    this.stroke(this.mask, from, to, brush ? '#fff' : '#000', 'source-over');
    this.stroke(this.view, from, to, '#f33', brush ? 'source-over' : 'destination-out');
    this.cursor = to;
  }

  private stroke(canvas: HTMLCanvasElement, from: Point, to: Point, color: string, op: GlobalCompositeOperation): void {
    const g = canvas.getContext('2d');
    if (!g) return;
    g.save();
    g.globalCompositeOperation = op;
    g.fillStyle = color;
    g.strokeStyle = color;
    g.lineWidth = this.size;
    g.lineCap = 'round';
    g.beginPath();
    g.arc(from.x, from.y, this.size / 2, 0, Math.PI * 2);
    g.fill();
    g.beginPath();
    g.moveTo(from.x, from.y);
    g.lineTo(to.x, to.y);
    g.stroke();
    g.restore();
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

  private toolButton(label: string, tool: 'brush' | 'eraser') {
    const on = this.tool === tool;
    return html`<button type="button" class="${BUTTON} ${on ? SELECTED : ''}" aria-pressed=${String(on)} @click=${() => this.setTool(tool)}>${label}</button>`;
  }

  protected override render() {
    return html`
      <div class="flex flex-col gap-2" data-testid="gt-mask-paint">
        <div role="group" aria-label="描き方" class="flex flex-wrap items-center gap-2">
          ${this.toolButton('筆', 'brush')} ${this.toolButton('消しゴム', 'eraser')}
          <button type="button" class=${BUTTON} @click=${() => this.clear()}>全部消す</button>
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
        </label>
        <div class="relative inline-block max-w-full">
          ${this.src ? html`<img src=${this.src} alt="" class="block max-w-full" />` : ''}
          <canvas
            tabindex="0"
            aria-label="描き直す部分。白く塗ったところが描き直されます"
            class=${this.src ? 'absolute inset-0 h-full w-full' : ''}
            style="touch-action: none; cursor: crosshair; opacity: 0.55"
            @pointerdown=${this.down}
            @pointermove=${this.move}
            @pointerup=${this.up}
            @pointercancel=${this.up}
            @keydown=${this.key}
          ></canvas>
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'mam-mask-paint': MamMaskPaint;
  }
}
