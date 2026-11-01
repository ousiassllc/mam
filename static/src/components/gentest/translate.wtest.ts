import { expect } from '@open-wc/testing';
import { setViewport } from '@web/test-runner-commands';

// The translation switch of the generation test (#913): a checkbox, on for a fresh form, with a short label whose
// explanation is behind its own "?" (#906). The markup is what ComposeForm renders
// (backend/internal/web/layout_fixture_test.go keeps it current), drawn with the real Tailwind CSS at 375 px.
describe('the translation switch of the generation test (spec/components/gentest-screen.md)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  const box = () => host.querySelector<HTMLInputElement>('[data-testid="gt-translate-input"]')!;
  const label = () => host.querySelector<HTMLLabelElement>('[data-testid="gt-translate"]')!;

  beforeEach(async function () {
    this.timeout(60_000);
    stylesheet = document.createElement('link');
    stylesheet.rel = 'stylesheet';
    stylesheet.href = '/__tailwind.css';
    const loaded = new Promise<void>((resolve, reject) => {
      stylesheet.onload = () => resolve();
      stylesheet.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
    });
    document.head.append(stylesheet);
    await loaded;
    await setViewport({ width: 375, height: 800 });
    const html = await (await fetch('/src/components/testdata/layout-gentest-image.html')).text();
    host = document.createElement('div');
    host.style.padding = '16px';
    host.innerHTML = html;
    document.body.append(host);
  });

  afterEach(() => {
    host.remove();
    stylesheet.remove();
  });

  it('is a checkbox named translate that is on for a fresh form, and a tap on its label turns it off and on', () => {
    expect(box().type).to.equal('checkbox');
    expect(box().name).to.equal('translate');
    expect(box().value).to.equal('1');
    expect(box().checked, 'the default is on').to.equal(true);

    label().click();
    expect(box().checked).to.equal(false);
    label().click();
    expect(box().checked).to.equal(true);
  });

  it('has a short label, and its explanation is in a help panel that is not drawn until it is opened', () => {
    expect(label().textContent!.trim()).to.equal('英語に訳して送る');
    const toggle = host.querySelector<HTMLButtonElement>('button[aria-label="英訳の説明"]')!;
    expect(toggle, 'its own ? button').to.not.equal(null);
    const panel = host.querySelector<HTMLElement>(`#${toggle.getAttribute('aria-controls')}`)!;
    expect(panel.hasAttribute('popover')).to.equal(true);
    expect(panel.textContent).to.contain('Prompt Planner');
    expect(panel.textContent).to.contain('日本語のまま送らず');
    expect(getComputedStyle(panel).display, 'the panel is hidden while it is closed').to.equal('none');
    expect(label().textContent).to.not.contain('Prompt Planner');
  });

  it('does not make the form scroll sideways at 375 px', () => {
    const form = host.querySelector<HTMLElement>('[data-testid="gt-form"]')!;
    expect(form.scrollWidth).to.be.at.most(form.clientWidth);
    expect(document.documentElement.scrollWidth).to.be.at.most(document.documentElement.clientWidth);
  });
});
