import { expect } from '@open-wc/testing';
import { choose, markCurrent } from './model-pick';

// #1011: a model is chosen in the picker by a tap on its card; the card sets the values it carries (data-mp-values) in the hidden inputs of the field and the field then
// shows the card. The card of the model that is in the field looks chosen in the picker.
describe('the model picker puts the model of a card into its field (#1011)', () => {
  let host: HTMLElement;

  const FIELD = `<div data-model-pick-field>
    <input type="hidden" name="primary.provider" value="openai" data-mp-from="provider">
    <input type="hidden" name="primary.model" value="gpt-5" data-mp-from="model">
    <button type="button"><span data-mp-current><span data-testid="shown">gpt-5</span></span></button>
  </div>`;
  const card = (provider: string, model: string) =>
    `<button type="button" data-mp-choose data-mp-values='${JSON.stringify({ provider, model })}' class="border-neutral-800 bg-neutral-900"><span data-testid="name">${model}</span></button>`;

  const field = () => host.querySelector<HTMLElement>('[data-model-pick-field]')!;
  const value = (name: string) => host.querySelector<HTMLInputElement>(`input[name="${name}"]`)!.value;

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = `${FIELD}<div id="body">${card('openai', 'gpt-5')}${card('anthropic', 'claude-x')}</div>`;
    document.body.append(host);
  });

  afterEach(() => host.remove());

  it('sets every input of the field from the card, shows the card and tells the form it changed', () => {
    let changed = 0;
    field().addEventListener('change', () => changed++);

    const [, second] = [...host.querySelectorAll<HTMLElement>('[data-mp-choose]')] as [HTMLElement, HTMLElement];
    choose(second, field());

    expect(value('primary.provider')).to.equal('anthropic');
    expect(value('primary.model')).to.equal('claude-x');
    expect(host.querySelector('[data-mp-current]')!.textContent).to.equal('claude-x');
    expect(changed).to.equal(1);
  });

  it('marks only the card of the model that is in the field, and looks chosen', () => {
    const cards = [...host.querySelectorAll<HTMLElement>('[data-mp-choose]')] as [HTMLElement, HTMLElement];

    markCurrent(host.querySelector('#body')!, field());

    expect(cards[0].getAttribute('aria-pressed')).to.equal('true');
    expect(cards[0].classList.contains('border-indigo-400')).to.equal(true);
    expect(cards[0].classList.contains('border-neutral-800')).to.equal(false);
    expect(cards[1].getAttribute('aria-pressed')).to.equal('false');
    expect(cards[1].classList.contains('border-neutral-800')).to.equal(true);
  });
});
