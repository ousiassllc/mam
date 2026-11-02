import { LitElement, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { factsOf, modelWeightOf, type Facts } from './weight-facts';
import { adjust, read, WEIGHT_MAX, WEIGHT_MIN, type Direction, type Reading } from './weight-model';

const PROMPT = 'textarea[name="prompt"]';
const CHOSEN = '[data-model] input[name="model"]:checked:not(:disabled)';

const BUTTON =
  'inline-flex items-center justify-center rounded-md border border-neutral-600 px-2 py-1 text-xs font-medium text-neutral-200 transition hover:bg-neutral-800 ' +
  'aria-disabled:cursor-not-allowed aria-disabled:opacity-50 ' +
  'focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-indigo-400 focus-visible:ring-offset-2 focus-visible:ring-offset-neutral-950';

/**
 * `<mam-prompt-weight>` is the "強める + / 弱める −" of the words of a prompt (spec/components/gentest-tiers.md, Issues #977 and #972).
 * It stands in the prompt fieldset of the easy and detail tiers (`scope` is the selector of the ancestor it watches) and works on the prompt
 * boxes in it (textarea[name=prompt]; the negative prompt has no weights of its own). With words selected in one, a small bar under the boxes
 * says what they weigh now and puts "(語:1.2)" around them, a step of 0.1 at a time, between 0.5 and 1.5; the selection stays on the changed part,
 * so that the button can be pressed again. Ctrl/⌘ + ↑ / ↓ does the same from the box. Whether the chosen models read weights comes from their cards
 * (weight-facts.ts): when none does, the buttons are off and the note says why (it is the same wording as the "?" of the prompts).
 * The text rules are weight-model.ts. It renders into the light DOM (the page's Tailwind classes apply) and hides itself while nothing is selected.
 */
@customElement('mam-prompt-weight')
export class MamPromptWeight extends LitElement {
  @property({ type: String }) scope = 'fieldset';

  @state() private reading: Reading | null = null;
  @state() private notice = '';

  private area: Element | null = null;
  private form: HTMLFormElement | null = null;
  private target: HTMLTextAreaElement | null = null;

  protected override createRenderRoot(): HTMLElement {
    this.replaceChildren(); // a history snapshot keeps the bar that was drawn: this element draws its own
    return this;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.area = this.closest(this.scope);
    this.form = this.closest('form');
    for (const type of ['select', 'selectionchange', 'keyup', 'mouseup', 'input', 'focusin'] as const)
      this.area?.addEventListener(type, this.onSelect);
    this.area?.addEventListener('keydown', this.onKey as EventListener);
    this.area?.addEventListener('focusout', this.onFocusOut as EventListener);
    this.form?.addEventListener('change', this.onModels);
    document.addEventListener('commercial-filter-applied', this.onModels);
  }

  override disconnectedCallback(): void {
    for (const type of ['select', 'selectionchange', 'keyup', 'mouseup', 'input', 'focusin'] as const)
      this.area?.removeEventListener(type, this.onSelect);
    this.area?.removeEventListener('keydown', this.onKey as EventListener);
    this.area?.removeEventListener('focusout', this.onFocusOut as EventListener);
    this.form?.removeEventListener('change', this.onModels);
    document.removeEventListener('commercial-filter-applied', this.onModels);
    super.disconnectedCallback();
  }

  /** How the chosen models read weights: from the cards of the form (the easy tier's hidden one included). */
  private facts(): Facts {
    const cards = [...(this.form?.querySelectorAll<HTMLInputElement>(CHOSEN) ?? [])];
    return factsOf(cards.map((input) => modelWeightOf(input.closest<HTMLElement>('[data-model]')!.dataset)));
  }

  private onModels = (): void => this.requestUpdate();

  private static promptBox(el: EventTarget | null): HTMLTextAreaElement | null {
    return el instanceof HTMLTextAreaElement && el.matches(PROMPT) ? el : null;
  }

  /** Looks at what is selected in a prompt box and shows or hides the bar. */
  private refresh(box: HTMLTextAreaElement): void {
    const r = read(box.value, box.selectionStart, box.selectionEnd);
    this.target = r ? box : null;
    this.reading = r;
    this.notice = '';
  }

  private onSelect = (event: Event): void => {
    const box = MamPromptWeight.promptBox(event.target);
    if (box) this.refresh(box);
  };

  private onFocusOut = (event: FocusEvent): void => {
    const leaving =
      event.target instanceof Node && (this.contains(event.target) || MamPromptWeight.promptBox(event.target));
    if (!leaving) return;
    // The bar keeps the selection (a box does not lose it when it loses the focus): it stays while the focus is in the bar or goes to a prompt box.
    const to = event.relatedTarget;
    if (to instanceof Node && (this.contains(to) || MamPromptWeight.promptBox(to))) return;
    this.target = null;
    this.reading = null;
  };

  private onKey = (event: KeyboardEvent): void => {
    const box = MamPromptWeight.promptBox(event.target);
    if (!box) return;
    if (event.key === 'Escape' && this.reading) {
      event.preventDefault();
      this.target = null;
      this.reading = null;
      return;
    }
    if (!(event.ctrlKey || event.metaKey) || (event.key !== 'ArrowUp' && event.key !== 'ArrowDown')) return;
    this.refresh(box);
    if (!this.reading) return;
    event.preventDefault();
    this.press(event.key === 'ArrowUp' ? 1 : -1);
  };

  /** Puts the change into the box as an edit the browser can undo while the box has the focus; the selection ends on the changed part. */
  private write(box: HTMLTextAreaElement, start: number, replacedEnd: number, text: string): void {
    if (document.activeElement === box) {
      box.setSelectionRange(start, replacedEnd);
      if (document.execCommand('insertText', false, text)) return;
    }
    box.setRangeText(text, start, replacedEnd, 'select');
    box.dispatchEvent(new Event('input', { bubbles: true }));
  }

  private press(dir: Direction): void {
    const box = this.target;
    if (!box || !this.facts().usable) return;
    const change = adjust(box.value, box.selectionStart, box.selectionEnd, dir);
    if (!change) return;
    if (box.maxLength >= 0 && change.value.length > box.maxLength) {
      this.notice = `文字数の上限（${box.maxLength} 文字）を超えるので、ウェイトは付けられません`;
      return;
    }
    this.write(box, change.start, change.replacedEnd, change.value.slice(change.start, change.end));
    box.setSelectionRange(change.start, change.end);
    this.refresh(box);
  }

  protected override updated(): void {
    this.hidden = !this.reading;
  }

  private button(label: string, dir: Direction, off: boolean, noteID: string) {
    return html`<button
      type="button"
      class=${BUTTON}
      aria-disabled=${off ? 'true' : 'false'}
      aria-describedby=${noteID}
      data-gt-weight=${dir > 0 ? 'up' : 'down'}
      data-testid=${dir > 0 ? 'gt-weight-up' : 'gt-weight-down'}
      @click=${() => !off && this.press(dir)}
    >
      ${label}
    </button>`;
  }

  protected override render() {
    const r = this.reading;
    if (!r) return nothing;
    const facts = this.facts();
    const notes = this.notice ? [...facts.notes, this.notice] : facts.notes;
    const noteID = `${this.id || 'gt-weight'}-note`;
    return html`<div
      role="group"
      aria-label="選んだ語のウェイト"
      class="flex flex-wrap items-center gap-2 rounded-md border border-neutral-700 bg-neutral-900 p-2 text-xs text-neutral-300"
      data-testid="gt-weight-bar"
      @mousedown=${(e: Event) => e.preventDefault()}
    >
      <span class="text-neutral-400">選んだ語の強さ <output class="tabular-nums text-neutral-100" data-testid="gt-weight-now">${r.weight.toFixed(1)}</output> 倍</span>
      ${this.button('強める +', 1, !facts.usable || !r.canRaise, noteID)} ${this.button('弱める −', -1, !facts.usable || !r.canLower, noteID)}
      <span class="text-neutral-400">${WEIGHT_MIN}〜${WEIGHT_MAX}。Ctrl/⌘ + ↑ ↓ でも変えられます</span>
      <div id=${noteID} role="status" class="basis-full" data-testid="gt-weight-note">
        ${notes.map((n) => html`<p class="text-amber-300">${n}</p>`)}
      </div>
    </div>`;
  }
}
