import { expect, waitUntil } from '@open-wc/testing';
import { sendKeys, sendMouse, setViewport } from '@web/test-runner-commands';
import '../../main';

describe('"?" help popover (spec/components/shared-ui-parts.md §9)', () => {
  let host: HTMLElement;
  const toggle = (n = 1) => host.querySelector<HTMLButtonElement>(`#t${n}`)!;
  const panel = (n = 1) => host.querySelector<HTMLElement>(`#p${n}`)!;
  const isOpen = (n = 1) => panel(n).matches(':popover-open');
  // The popover is open once the browser says so and the script has answered its `toggle` (aria-expanded, the place).
  const opened = (n = 1) =>
    waitUntil(() => isOpen(n) && toggle(n).getAttribute('aria-expanded') === 'true', `help ${n} did not open`);
  const closed = (n = 1) =>
    waitUntil(() => !isOpen(n) && toggle(n).getAttribute('aria-expanded') === 'false', `help ${n} did not close`);

  /** The middle of an element, to click with a real pointer (a synthetic click() does not light-dismiss a popover). */
  function centre(el: Element): [number, number] {
    const r = el.getBoundingClientRect();
    return [Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2)];
  }

  beforeEach(async () => {
    await setViewport({ width: 800, height: 600 });
    host = document.createElement('div');
    host.style.padding = '20px';
    host.innerHTML = `
      <section style="overflow:hidden">
        <h2 style="display:inline">見出し 1</h2>
        <button type="button" id="t1" data-help-toggle popovertarget="p1" aria-expanded="false" aria-controls="p1" aria-label="見出し 1の説明">?</button>
        <div id="p1" popover role="group" aria-label="見出し 1の説明" data-help-panel style="box-sizing:border-box;width:min(24rem,calc(100vw - 1rem));margin:0;inset:auto"><p>説明 1</p><a id="link1" href="#x">リンク</a></div>
      </section>
      <section>
        <button type="button" id="t2" data-help-toggle popovertarget="p2" aria-expanded="false" aria-controls="p2" aria-label="見出し 2の説明">?</button>
        <div id="p2" popover role="group" aria-label="見出し 2の説明" data-help-panel style="box-sizing:border-box;width:min(24rem,calc(100vw - 1rem));margin:0;inset:auto"><p>説明 2</p></div>
      </section>
      <button type="button" id="after">次のボタン</button>
      <p id="text" style="margin-top:300px">本文</p>`;
    document.body.append(host);
  });
  afterEach(async () => {
    host.remove();
    await setViewport({ width: 800, height: 600 });
  });

  it('is closed at first, opens on click and tells aria-expanded', async () => {
    expect(isOpen()).to.equal(false);
    expect(toggle().getAttribute('aria-expanded')).to.equal('false');

    toggle().click();
    await opened();

    expect(toggle().getAttribute('aria-expanded')).to.equal('true');
  });

  it('closes on a second click of the button', async () => {
    toggle().click();
    await opened();

    toggle().click();
    await closed();

    expect(toggle().getAttribute('aria-expanded')).to.equal('false');
  });

  it('opens and closes from the keyboard (Enter, Space) and keeps the focus on the button', async () => {
    toggle().focus();

    await sendKeys({ press: 'Enter' });
    await opened();
    expect(document.activeElement === toggle()).to.equal(true);

    await sendKeys({ press: 'Space' });
    await closed();
    expect(document.activeElement === toggle()).to.equal(true);
  });

  it('closes on Esc from the button and keeps the focus on it', async () => {
    toggle().focus();
    await sendKeys({ press: 'Enter' });
    await opened();

    await sendKeys({ press: 'Escape' });
    await closed();

    expect(toggle().getAttribute('aria-expanded')).to.equal('false');
    expect(document.activeElement === toggle()).to.equal(true);
  });

  it('closes on Esc from a link inside it and returns the focus to the button', async () => {
    toggle().focus();
    await sendKeys({ press: 'Enter' });
    await opened();
    await sendKeys({ press: 'Tab' });
    expect(document.activeElement === host.querySelector('#link1')).to.equal(true);

    await sendKeys({ press: 'Escape' });
    await closed();

    expect(document.activeElement === toggle()).to.equal(true);
  });

  it('closes on Esc when the focus is on the page (a click on its text), and returns the focus to the button', async () => {
    toggle().click();
    await opened();
    (document.activeElement as HTMLElement | null)?.blur();

    await sendKeys({ press: 'Escape' });
    await closed();

    expect(document.activeElement === toggle()).to.equal(true);
  });

  it('closes on a click outside it', async () => {
    toggle().click();
    await opened();

    await sendMouse({ type: 'click', position: centre(host.querySelector('#text')!) });
    await closed();

    expect(toggle().getAttribute('aria-expanded')).to.equal('false');
  });

  it('keeps open on a click inside it', async () => {
    toggle().click();
    await opened();

    await sendMouse({ type: 'click', position: centre(panel().querySelector('p')!) });

    expect(isOpen()).to.equal(true);
  });

  it('has one help open at a time: opening another closes the first', async () => {
    toggle().click();
    await opened();

    toggle(2).click();
    await opened(2);
    await closed();

    expect(toggle().getAttribute('aria-expanded')).to.equal('false');
    expect(toggle(2).getAttribute('aria-expanded')).to.equal('true');
  });

  it('goes from the button into the popover with Tab, and closes when the focus leaves it', async () => {
    toggle().focus();
    await sendKeys({ press: 'Enter' });
    await opened();

    await sendKeys({ press: 'Tab' });
    expect(document.activeElement === host.querySelector('#link1')).to.equal(true);
    expect(isOpen()).to.equal(true);

    await sendKeys({ press: 'Tab' });
    await closed();
    expect(toggle().getAttribute('aria-expanded')).to.equal('false');
  });

  it('closes when Shift+Tab takes the focus before the button', async () => {
    const before = document.createElement('button');
    host.prepend(before);
    toggle().focus();
    await sendKeys({ press: 'Enter' });
    await opened();

    await sendKeys({ down: 'Shift' });
    await sendKeys({ press: 'Tab' });
    await sendKeys({ up: 'Shift' });
    await closed();
  });

  it('is not clipped by a parent with overflow hidden: it is in the top layer under the button', async () => {
    toggle().click();
    await opened();

    const at = toggle().getBoundingClientRect();
    const p = panel().getBoundingClientRect();
    expect(p.top).to.be.at.least(at.bottom);
    expect(Math.abs(p.left - at.left)).to.be.lessThan(2);
    expect(host.querySelector('section')!.contains(document.elementFromPoint(p.left + 5, p.top + 5))).to.equal(true);
  });

  for (const [name, width, height] of [
    ['desktop', 1280, 700],
    ['narrow', 375, 700],
  ] as const) {
    it(`stays inside the window at ${name} width wherever the button is`, async () => {
      await setViewport({ width, height });
      // The button at the right edge: the popover is shifted left; near the bottom: it is flipped above.
      host.style.cssText = 'position:fixed;inset:0;padding:0';
      const t = toggle(2);
      for (const [x, y] of [
        ['auto', 20],
        [width - 40, 20],
        [width - 40, height - 40],
        [10, height - 40],
      ] as const) {
        const section = t.parentElement!;
        section.style.cssText = `position:fixed;${x === 'auto' ? 'left:10px' : `left:${x}px`};top:${y}px`;
        t.click();
        await opened(2);
        const r = panel(2).getBoundingClientRect();
        expect(r.left, `left at ${x},${y}`).to.be.at.least(0);
        expect(r.right, `right at ${x},${y}`).to.be.at.most(document.documentElement.clientWidth);
        expect(r.top, `top at ${x},${y}`).to.be.at.least(0);
        expect(r.bottom, `bottom at ${x},${y}`).to.be.at.most(window.innerHeight);
        expect(document.documentElement.scrollWidth).to.be.at.most(document.documentElement.clientWidth);
        t.click();
        await closed(2);
      }
    });
  }
});
