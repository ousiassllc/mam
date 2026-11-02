import { LitElement, html } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

/**
 * `<mam-mask-paint>` paints the part of a picture to redraw (spec/components/gentest-screen.md, #974).
 *
 * The mask is a black canvas (keep) with white strokes (redraw). The view shows those strokes in red over
 * the source image, so black is not drawn as red. `maskPNG()` exports the black/white PNG. On the surrounding
 * form's submit the PNG is placed in `input[type=file][name=mask]` before the form is sent.
 *
 * Keyboard: B brush, E eraser, [ ] thickness, arrows stamp one dab. The canvas is in the tab order.
 * It renders into the light DOM so the page's Tailwind classes apply.
 */
@customElement('mam-mask-paint')
export class MamMaskPaint extends LitElement {
  @property({ type: String }) src = '';
  @property({ type: String, attribute: 'source-input' }) sourceInput = '';
  @property({ type: Number }) width = 64;
  @property({ type: Number }) height = 64;

  @state() private tool: 'brush' | 'eraser' = 'brush';
  @state() private size = 16;

  private view?: HTMLCanvasElement;
  private mask?: HTMLCanvasElement;
  private form: HTMLFormElement | null = null;
  private objectURL = '';
  private drawing = false;
  private cursor = { x: 32, y: 32 };
  private builtFor = '';

  protected override createRenderRoot(): HTMLElement {
    return this;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.form = this.closest('form');
    this.form?.addEventListener('submit', this.onSubmit);
    const input = this.sourceInput ? document.getElementById(this.sourceInput) : null;
    input?.addEventListener('change', this.onFile);
  }

  override disconnectedCallback(): void {
    this.form?.removeEventListener('submit', this.onSubmit);
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
      this.resize(img.naturalWidth || this.width, img.naturalHeight || this.height, src);
    };
    img.src = src;
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
    this.size = Math.min(128, Math.max(1, Math.round(n)));
  }

  private clear(): void {
    if (!this.view || !this.mask) return;
    this.resize(this.view.width, this.view.height, this.builtFor);
  }

  private pos(e: PointerEvent): { x: number; y: number } {
    const c = this.view!;
    const r = c.getBoundingClientRect();
    const sx = r.width > 0 ? c.width / r.width : 1;
    const sy = r.height > 0 ? c.height / r.height : 1;
    return { x: (e.clientX - r.left) * sx, y: (e.clientY - r.top) * sy };
  }

  private down = (e: PointerEvent): void => {
    this.drawing = true;
    (e.currentTarget as HTMLCanvasElement).setPointerCapture?.(e.pointerId);
    this.stamp(this.pos(e));
  };

  private move = (e: PointerEvent): void => {
    if (!this.drawing) return;
    this.stamp(this.pos(e));
  };

  private up = (): void => {
    this.drawing = false;
  };

  private stamp(at: { x: number; y: number }): void {
    if (!this.view || !this.mask) return;
    if (this.form) this.form.dataset.maskReady = '';
    const r = this.size / 2;
    const m = this.mask.getContext('2d');
    const v = this.view.getContext('2d');
    if (!m || !v) return;
    m.fillStyle = this.tool === 'brush' ? '#fff' : '#000';
    m.beginPath();
    m.arc(at.x, at.y, r, 0, Math.PI * 2);
    m.fill();
    v.save();
    if (this.tool === 'eraser') v.globalCompositeOperation = 'destination-out';
    else v.globalAlpha = 0.55;
    v.fillStyle = this.tool === 'brush' ? '#f33' : '#000';
    v.beginPath();
    v.arc(at.x, at.y, r, 0, Math.PI * 2);
    v.fill();
    v.restore();
    this.cursor = at;
  }

  private key = (e: KeyboardEvent): void => {
    const step = Math.max(1, this.size);
    if (e.key === 'b' || e.key === 'B') this.setTool('brush');
    else if (e.key === 'e' || e.key === 'E') this.setTool('eraser');
    else if (e.key === '[') this.setSize(this.size - 2);
    else if (e.key === ']') this.setSize(this.size + 2);
    else if (e.key === 'ArrowLeft') this.stamp({ x: this.cursor.x - step, y: this.cursor.y });
    else if (e.key === 'ArrowRight') this.stamp({ x: this.cursor.x + step, y: this.cursor.y });
    else if (e.key === 'ArrowUp') this.stamp({ x: this.cursor.x, y: this.cursor.y - step });
    else if (e.key === 'ArrowDown') this.stamp({ x: this.cursor.x, y: this.cursor.y + step });
    else return;
    e.preventDefault();
  };

  private onSubmit = (e: Event): void => {
    const form = e.currentTarget as HTMLFormElement;
    if (form.dataset.maskReady === '1') {
      form.dataset.maskReady = '';
      return;
    }
    e.preventDefault();
    void this.maskPNG().then((blob) => {
      const input = form.querySelector<HTMLInputElement>('input[type="file"][name="mask"]');
      if (input) {
        const dt = new DataTransfer();
        dt.items.add(new File([blob], 'mask.png', { type: 'image/png' }));
        input.files = dt.files;
      }
      form.dataset.maskReady = '1';
      form.requestSubmit();
    });
  };

  protected override render() {
    const brush = this.tool === 'brush';
    return html`
      <div class="flex flex-col gap-2" data-testid="gt-mask-paint">
        <div role="radiogroup" aria-label="描き方" class="flex gap-2">
          <button type="button" role="radio" aria-checked=${String(brush)} @click=${() => this.setTool('brush')}>筆</button>
          <button type="button" role="radio" aria-checked=${String(!brush)} @click=${() => this.setTool('eraser')}>消しゴム</button>
        </div>
        <label class="flex items-center gap-2 text-xs text-neutral-400">
          太さ
          <input type="range" min="1" max="128" .value=${String(this.size)} aria-label="太さ" @input=${(e: Event) => this.setSize(Number((e.target as HTMLInputElement).value))} />
        </label>
        <button type="button" @click=${() => this.clear()}>全部消す</button>
        <div class="relative inline-block max-w-full">
          ${this.src ? html`<img src=${this.src} alt="" class="block max-w-full" />` : ''}
          <canvas
            tabindex="0"
            aria-label="描き直す部分。白く塗ったところが描き直されます"
            class=${this.src ? 'absolute inset-0 h-full w-full' : ''}
            style="touch-action: none; cursor: crosshair"
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
