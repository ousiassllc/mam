import { expect } from '@open-wc/testing';
import { sendKeys, setViewport } from '@web/test-runner-commands';

// The model cards of the generation test (#906): a card is tapped to select it (the checkbox is hidden but is still the
// control), the selection is shown by a glowing border and a mark, the keyboard and a screen reader get a checkbox.
// The markup is what ComposeForm renders (backend/internal/web/layout_fixture_test.go keeps it current), drawn with the
// real Tailwind CSS.
describe('the model cards of the generation test (spec/components/gentest-screen.md)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  const input = (id: string) => host.querySelector<HTMLInputElement>(`[data-model="${id}"] input`)!;
  const label = (id: string) => host.querySelector<HTMLLabelElement>(`[data-model="${id}"] label`)!;
  const face = (id: string) => label(id).querySelector<HTMLElement>('span')!;
  const mark = (id: string) => label(id).querySelector<HTMLElement>('[data-gt-model-mark]')!;

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

  it('selects and unselects a card by tapping it, and more than one can be selected', () => {
    expect(input('nano-banana').checked).to.equal(true);
    expect(input('gpt-image-2').checked).to.equal(false);

    label('gpt-image-2').click();
    expect(input('gpt-image-2').checked).to.equal(true);
    expect(input('nano-banana').checked, 'the first stays selected').to.equal(true);

    label('nano-banana').click();
    expect(input('nano-banana').checked).to.equal(false);
  });

  it('shows the selected card with a glowing border and a mark, and not the others', () => {
    const selected = getComputedStyle(face('nano-banana'));
    const plain = getComputedStyle(face('gpt-image-2'));

    expect(selected.boxShadow, 'the selected card glows').to.not.equal('none');
    expect(plain.boxShadow, 'a card that is not selected does not').to.equal('none');
    expect(selected.borderTopColor, 'the border of the selected card differs').to.not.equal(plain.borderTopColor);
    expect(getComputedStyle(mark('nano-banana')).display, 'the mark (not colour alone)').to.not.equal('none');
    expect(getComputedStyle(mark('gpt-image-2')).display).to.equal('none');
  });

  it('is a checkbox for the keyboard and the screen reader, toggled by Space, with a focus ring', async () => {
    const box = input('gpt-image-2');
    expect(box.type).to.equal('checkbox');
    box.focus();
    await sendKeys({ press: 'Space' });
    expect(box.checked).to.equal(true);
    await sendKeys({ press: 'Space' });
    expect(box.checked).to.equal(false);

    // :focus-visible matches after a key press, and the card then shows a ring of its own (a box-shadow).
    box.focus();
    await sendKeys({ press: 'Shift' });
    expect(box.matches(':focus-visible')).to.equal(true);
    expect(getComputedStyle(face('gpt-image-2')).boxShadow).to.not.equal('none');
  });

  it('cannot select a model that cannot be called, and says so to a screen reader without a yellow line', () => {
    const lite = input('nano-banana-lite');
    expect(lite.disabled).to.equal(true);
    label('nano-banana-lite').click();
    expect(lite.checked).to.equal(false);

    const reason = host.querySelector<HTMLElement>(`#${lite.getAttribute('aria-describedby')}`)!;
    expect(reason.textContent).to.contain('API キー');
    expect(reason.getBoundingClientRect().width, 'the reason is not drawn on the card').to.be.at.most(1);
  });
});
