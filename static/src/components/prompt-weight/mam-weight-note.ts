import { html, LitElement, nothing } from 'lit';
import { customElement } from 'lit/decorators.js';
import { chosenFacts, watchChosen } from './chosen-models';
import { cautionOf } from './weight-facts';

/**
 * `<mam-weight-note>` is the short caution under the prompt boxes while a chosen model does not read the weights, "(語:1.3)" (Issue #1023): it is known
 * without opening the "?". Hidden while every chosen model reads them (or none is chosen). It reads the cards of the form and follows the choice.
 * It renders into the light DOM.
 */
@customElement('mam-weight-note')
export class MamWeightNote extends LitElement {
  private stop: (() => void) | null = null;

  protected override createRenderRoot(): HTMLElement {
    this.replaceChildren(); // a history snapshot keeps what was drawn: this element draws its own
    return this;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.stop = watchChosen(this.closest('form'), () => this.requestUpdate());
  }

  override disconnectedCallback(): void {
    this.stop?.();
    super.disconnectedCallback();
  }

  private text(): string {
    return cautionOf(chosenFacts(this.closest('form')));
  }

  protected override updated(): void {
    this.hidden = this.text() === '';
  }

  protected override render() {
    const text = this.text();
    return text === ''
      ? nothing
      : html`<p class="text-xs text-amber-300" role="status" data-testid="gt-weight-caution-text">${text}</p>`;
  }
}
