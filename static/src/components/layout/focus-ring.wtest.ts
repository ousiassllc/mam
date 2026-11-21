import { expect } from '@open-wc/testing';
import { sendKeys } from '@web/test-runner-commands';

// C3: a control that says nothing about its focus ring gets the guide's one (2px solid --focus, offset 2), not the browser's own ring.
// tokens.css puts `:where(a, button, summary, [tabindex]):focus-visible` in @layer base; a utility on the control (focus-visible:outline-none,
// focus-visible:focus-ring) is a later layer and wins. Drawn with the real Tailwind CSS (web-test-runner.config.mjs builds it from src/css/app.css).
describe('the focus ring of a control without a class (C3)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  beforeEach(async function () {
    this.timeout(60_000); // the first request waits for the build of the CSS
    stylesheet = document.createElement('link');
    stylesheet.rel = 'stylesheet';
    stylesheet.href = '/__tailwind.css';
    const loaded = new Promise<void>((resolve, reject) => {
      stylesheet.onload = () => resolve();
      stylesheet.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
    });
    document.head.append(stylesheet);
    await loaded;
    host = document.createElement('div');
    host.className = 'p-4';
    host.innerHTML = `
      <a id="link" class="underline" href="#x">リンク</a>
      <button id="button" type="button" class="rounded-md border border-border px-3">ボタン</button>
      <details><summary id="summary" class="cursor-pointer text-mute">開く</summary>中身</details>
      <div id="scroller" tabindex="0" class="overflow-auto">領域</div>
      <button id="quiet" type="button" class="focus-visible:outline-none">輪なし</button>
      <button id="own" type="button" class="focus-visible:focus-ring">輪あり</button>`;
    document.body.append(host);
  });

  afterEach(() => {
    host.remove();
    stylesheet.remove();
  });

  const focusOf = (id: string) => {
    const el = host.querySelector<HTMLElement>(`#${id}`)!;
    const style = getComputedStyle(el);
    return {
      style: style.outlineStyle,
      width: style.outlineWidth,
      offset: style.outlineOffset,
      color: style.outlineColor,
    };
  };

  async function tabTo(id: string): Promise<void> {
    host.querySelector<HTMLElement>('#link')!.focus();
    for (let i = 0; i < 10 && document.activeElement?.id !== id; i++) await sendKeys({ press: 'Tab' });
    expect(document.activeElement?.id).to.equal(id);
  }

  const focusColor = () => {
    const probe = document.createElement('span');
    probe.style.color = 'var(--focus)';
    host.append(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return color;
  };

  for (const id of ['link', 'button', 'summary', 'scroller']) {
    it(`draws 2px solid --focus, offset 2 on #${id} when the keyboard reaches it`, async () => {
      await tabTo(id);

      expect(focusOf(id)).to.deep.equal({ style: 'solid', width: '2px', offset: '2px', color: focusColor() });
    });
  }

  it('lets focus-visible:outline-none keep the ring off', async () => {
    await tabTo('quiet');

    expect(focusOf('quiet').style).to.not.equal('solid');
  });

  it('keeps the ring of focus-visible:focus-ring', async () => {
    await tabTo('own');

    expect(focusOf('own')).to.deep.equal({ style: 'solid', width: '2px', offset: '2px', color: focusColor() });
  });

  it('draws nothing while the control has no keyboard focus', () => {
    expect(focusOf('button').style).to.equal('none');
  });
});
