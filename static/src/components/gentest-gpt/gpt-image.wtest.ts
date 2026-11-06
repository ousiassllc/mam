import { expect } from '@open-wc/testing';
import { initGentestGptImage } from './gpt-image';

// The "GPT Image 2.6 の設定" section of the generation test form (#1096). The markup is what ComposeForm renders
// (backend/internal/web/layout_fixture_test.go keeps it current): the model cards carry data-gpt-image / data-gpt-refusal.
describe('the GPT Image 2.6 settings of the generation test form (spec/components/gentest-screen.md)', () => {
  let host: HTMLElement;
  const q = <T extends HTMLElement>(s: string) => host.querySelector<T>(s)!;
  const tick = (id: string, on: boolean) => {
    const input = host.querySelector<HTMLInputElement>(`[data-model="${id}"] input`)!;
    input.checked = on;
    input.dispatchEvent(new Event('change', { bubbles: true }));
  };
  const set = (el: HTMLSelectElement, value: string) => {
    el.value = value;
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  const section = () => q('[data-gt-gpt]');
  const format = () => q<HTMLSelectElement>('[data-testid="gt-gpt-format"]');
  const frames = () => q<HTMLInputElement>('[data-testid="gt-gpt-frames"]');
  const duration = () => q<HTMLInputElement>('[data-testid="gt-gpt-duration"]');

  before(() => initGentestGptImage());

  beforeEach(async () => {
    const html = await (await fetch('/src/components/testdata/layout-gentest-refs.html')).text();
    host = document.createElement('div');
    host.innerHTML = html;
    document.body.append(host);
    host.dispatchEvent(new Event('htmx:load', { bubbles: true }));
  });

  afterEach(() => host.remove());

  it('is hidden while no ticked model takes the settings (and posts nothing)', () => {
    expect(section().hidden).to.equal(true);
    expect(q<HTMLSelectElement>('[data-testid="gt-gpt-complexity"]').disabled).to.equal(true);
  });

  it('appears for GPT Image 2.6 and shows the frames only for a GIF', () => {
    tick('gpt', true);
    expect(section().hidden).to.equal(false);
    expect(q('[data-gt-gpt-fields]').hidden).to.equal(false);
    expect(q<HTMLSelectElement>('[data-testid="gt-gpt-complexity"]').disabled).to.equal(false);
    expect(q('[data-gt-gpt-frames]').hidden, 'PNG: no frames').to.equal(true);
    expect(frames().disabled).to.equal(true);
    expect(duration().disabled).to.equal(true);

    set(format(), 'gif');
    expect(q('[data-gt-gpt-frames]').hidden).to.equal(false);
    expect(frames().disabled).to.equal(false);
    expect(duration().disabled).to.equal(false);
    expect(q('[data-testid="gt-gpt-gif-hint"]').hidden).to.equal(false);
    expect(q('[data-testid="gt-gpt-svg-hint"]').hidden).to.equal(true);

    set(format(), 'svg');
    expect(q('[data-gt-gpt-frames]').hidden, 'SVG: no frames').to.equal(true);
    expect(frames().disabled).to.equal(true);
    expect(q('[data-testid="gt-gpt-svg-hint"]').hidden).to.equal(false);
    expect(q('[data-testid="gt-gpt-svg-hint"]').textContent).to.contain('費用は見積れません');
  });

  it('says why the ChatGPT-subscription twin cannot take them, without posting any setting for it', () => {
    tick('gpt-sub', true);
    expect(section().hidden).to.equal(false);
    expect(q('[data-gt-gpt-fields]').hidden).to.equal(true);
    expect(q('[data-gt-gpt-notes]').hidden).to.equal(false);
    expect(q('[data-gt-gpt-notes]').textContent).to.contain('GPT Image 2.6 Flare（ChatGPT）');
    expect(q('[data-gt-gpt-notes]').textContent).to.contain('API キー経由');
    expect(q<HTMLSelectElement>('[data-testid="gt-gpt-complexity"]').disabled).to.equal(true);
  });

  it('keeps a value that is set in view next to a model that cannot take it, and names the model', () => {
    tick('gpt', true);
    set(q<HTMLSelectElement>('[data-testid="gt-gpt-complexity"]'), 'low');
    tick('gpt', false);
    tick('i2i', true);
    expect(section().hidden, 'a value that is given is never hidden away').to.equal(false);
    expect(q<HTMLSelectElement>('[data-testid="gt-gpt-complexity"]').disabled).to.equal(false);
    expect(q('[data-gt-gpt-notes]').textContent).to.contain('SDXL');
  });
});
