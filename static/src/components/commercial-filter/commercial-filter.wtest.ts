import { expect } from '@open-wc/testing';
import { applyCommercialFilters, installCommercialFilter } from './commercial-filter';

// The filter of commercial use (#928) on the real markup of the generation test's model cards (layout-gentest-image.html, which
// backend/internal/web/layout_fixture_test.go keeps current): the cards carry data-commercial, the filter is a radio group
// naming the list it narrows.
describe('the commercial-use filter (spec/architecture/model-license.md)', () => {
  let host: HTMLElement;

  const card = (id: string) => host.querySelector<HTMLElement>(`[data-model="${id}"]`)!;
  const box = (id: string) => card(id).querySelector<HTMLInputElement>('input[name="model"]')!;
  const choose = (value: string) => {
    const radio = host.querySelector<HTMLInputElement>(`[data-commercial-filter] input[value="${value}"]`)!;
    radio.checked = true;
    radio.dispatchEvent(new Event('change', { bubbles: true }));
  };

  beforeEach(async () => {
    installCommercialFilter();
    const html = await (await fetch('/src/components/testdata/layout-gentest-image.html')).text();
    host = document.createElement('div');
    host.innerHTML = html;
    document.body.append(host);
  });

  afterEach(() => host.remove());

  it('has a card for each answer to start with, and shows all of them', () => {
    expect(card('nano-banana').dataset.commercial).to.equal('yes');
    expect(card('gpt-image-2').dataset.commercial).to.equal('unknown');
    for (const id of ['nano-banana', 'nano-banana-lite', 'gpt-image-2']) expect(card(id).hidden).to.equal(false);
  });

  it('hides the cards that are not 商用OK when 商用OKのみ is chosen, and disables them so that they post nothing', () => {
    choose('ok');

    expect(card('gpt-image-2').hidden, 'unknown is not 商用OK').to.equal(true);
    expect(box('gpt-image-2').disabled, 'a hidden card posts nothing').to.equal(true);
    expect(card('nano-banana').hidden).to.equal(false);
    expect(host.querySelector('[data-commercial-filter-status]')!.textContent).to.contain('1 件');
  });

  it('shows them again, enabled, when すべて is chosen', () => {
    choose('ok');
    choose('all');

    expect(card('gpt-image-2').hidden).to.equal(false);
    expect(box('gpt-image-2').disabled).to.equal(false);
    expect(host.querySelector('[data-commercial-filter-status]')!.textContent).to.equal('');
  });

  it('leaves a card that the server disabled disabled when the filter is widened', () => {
    // nano-banana-lite is "選べません": its checkbox is disabled by the server.
    expect(box('nano-banana-lite').disabled).to.equal(true);
    choose('ok');
    choose('all');

    expect(box('nano-banana-lite').disabled, 'only what the filter disabled is given back').to.equal(true);
  });

  it('tells the list that it was applied, so that the form can count what is ticked', () => {
    let applied = 0;
    host.addEventListener('commercial-filter-applied', () => applied++);

    choose('ok_or_conditional');

    expect(applied).to.equal(1);
    expect(card('gpt-image-2').hidden, 'unknown is neither 商用OK nor 条件つき').to.equal(true);
  });

  it('applies the checked filter when it is told to (a page drawn with the setting on)', () => {
    host.querySelector<HTMLInputElement>('[data-commercial-filter] input[value="ok"]')!.checked = true;

    applyCommercialFilters(host);

    expect(card('gpt-image-2').hidden).to.equal(true);
  });
});
