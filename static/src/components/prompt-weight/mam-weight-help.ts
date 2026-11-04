import { html, LitElement } from 'lit';
import { customElement } from 'lit/decorators.js';
import { chosenFacts, watchChosen } from './chosen-models';
import { lineOf } from './weight-facts';

/**
 * `<mam-weight-help>` is the part of the "?" of the prompts that says what the chosen models do with weights (Issue #1023): the models that are chosen
 * now and no others, by the name of their card (never an identifier), the ones that read the weights the same way on one line, and "未確認" for a model
 * whose way is not known (no long sentence repeated). It reads the cards of the form and follows the choice. It renders into the light DOM.
 */
@customElement('mam-weight-help')
export class MamWeightHelp extends LitElement {
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

  protected override render() {
    const facts = chosenFacts(this.closest('form'));
    if (facts.groups.length === 0)
      return html`<p class="text-neutral-400" data-testid="gt-weight-none">モデルを選ぶと、ここに効き方が出ます。</p>`;
    return html`<ul class="flex flex-col gap-1.5">
      ${facts.groups.map((g) => html`<li class="break-words" data-weight=${g.level}>${lineOf(g)}</li>`)}
    </ul>`;
  }
}
