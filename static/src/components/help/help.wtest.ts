import { expect, waitUntil } from '@open-wc/testing';
import { sendKeys, sendMouse, setViewport } from '@web/test-runner-commands';
import htmx from 'htmx.org';
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

  it('takes the Esc before an inline dialog that is open as well: the help closes, the dialog stays', async () => {
    host.insertAdjacentHTML(
      'beforeend',
      '<button type="button" id="ask" hx-get="/src/components/testdata/inline-dialog-open.html" hx-target="#mm-dialog" hx-swap="innerHTML">開く</button><div id="mm-dialog"></div>',
    );
    htmx.process(host);
    host.querySelector<HTMLButtonElement>('#ask')!.click();
    await waitUntil(() => host.querySelector('[data-inline-dialog]') !== null, 'the dialog did not arrive', {
      timeout: 2000,
    });
    toggle().click();
    await opened();
    (document.activeElement as HTMLElement | null)?.blur(); // the focus is on the page, as after a click on the text of the help

    await sendKeys({ press: 'Escape' });
    await closed();

    expect(host.querySelector('[data-inline-dialog]')).to.not.equal(null);
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

  describe('across a swap (a help of a polled card)', () => {
    const DATA = '/src/components/testdata';
    const swapCards = () => htmx.ajax('get', `${DATA}/help-swap-cards.html`, { target: '#cards', swap: 'outerHTML' });

    beforeEach(() => {
      const cards = document.createElement('div');
      cards.innerHTML = `
        <ul id="cards"><li><button type="button" id="t3" data-help-toggle popovertarget="p3" aria-expanded="false" aria-controls="p3" aria-label="カードの説明">?</button></li></ul>
        <div id="p3" popover role="group" aria-label="カードの説明" data-help-panel style="box-sizing:border-box;width:min(24rem,calc(100vw - 1rem));margin:0;inset:auto"><p>説明 3</p></div>
        <div id="region"></div>`;
      host.append(cards);
      htmx.process(cards);
    });

    it('stays open when its button is swapped, and the new button says so', async () => {
      const old = host.querySelector('#t3')!;
      old.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await waitUntil(() => host.querySelector('#p3')!.matches(':popover-open'), 'help 3 did not open');

      await swapCards();
      await waitUntil(() => host.querySelector('#t3') !== old, 'the cards were not swapped');

      const fresh = host.querySelector<HTMLButtonElement>('#t3')!;
      await waitUntil(() => fresh.getAttribute('aria-expanded') === 'true', 'the new button does not say it is open');
      expect(host.querySelector('#p3')!.matches(':popover-open')).to.equal(true);
    });

    it('is put by the new button, and closes on Esc with the focus on the new button', async () => {
      host.querySelector<HTMLButtonElement>('#t3')!.click();
      await waitUntil(() => host.querySelector('#p3')!.matches(':popover-open'), 'help 3 did not open');
      await swapCards();
      await waitUntil(() => host.querySelector('#t3')?.getAttribute('aria-expanded') === 'true', 'not synced');

      const p = host.querySelector<HTMLElement>('#p3')!.getBoundingClientRect();
      const at = host.querySelector('#t3')!.getBoundingClientRect();
      expect(p.top).to.be.at.least(at.bottom);

      await sendKeys({ press: 'Escape' });
      await waitUntil(() => !host.querySelector('#p3')!.matches(':popover-open'), 'Esc did not close it');

      expect(document.activeElement === host.querySelector('#t3')).to.equal(true);
      expect(host.querySelector('#t3')!.getAttribute('aria-expanded')).to.equal('false');
    });

    it('toggles from a real click on the new button: it closes, and a second click opens it again', async () => {
      host.querySelector<HTMLButtonElement>('#t3')!.click();
      await waitUntil(() => host.querySelector('#p3')!.matches(':popover-open'), 'help 3 did not open');
      await swapCards();
      await waitUntil(() => host.querySelector('#t3')?.getAttribute('aria-expanded') === 'true', 'not synced');

      await sendMouse({ type: 'click', position: centre(host.querySelector('#t3')!) });
      await waitUntil(
        () => !host.querySelector('#p3')!.matches(':popover-open'),
        'a click on the new button did not close it',
      );
      expect(host.querySelector('#t3')!.getAttribute('aria-expanded')).to.equal('false');

      await sendMouse({ type: 'click', position: centre(host.querySelector('#t3')!) });
      await waitUntil(() => host.querySelector('#p3')!.matches(':popover-open'), 'a second click did not open it');
    });

    it('closes a panel that is inside what is swapped: the new panel is a new, closed popover', async () => {
      host.insertAdjacentHTML(
        'beforeend',
        `<section id="inside"><button type="button" id="t4" data-help-toggle popovertarget="p4" aria-expanded="false" aria-controls="p4" aria-label="中の説明">?</button><div id="p4" popover role="group" aria-label="中の説明" data-help-panel style="margin:0;inset:auto"><p>説明 4</p></div></section>`,
      );
      host.querySelector<HTMLButtonElement>('#t4')!.click();
      await waitUntil(() => host.querySelector('#p4')!.matches(':popover-open'), 'help 4 did not open');

      await htmx.ajax('get', `${DATA}/help-swap-inside.html`, { target: '#inside', swap: 'outerHTML' });
      await waitUntil(
        () => host.querySelector('#p4') && !host.querySelector('#p4')!.matches(':popover-open'),
        'not closed',
      );

      expect(host.querySelector('#t4')!.getAttribute('aria-expanded')).to.equal('false');
    });
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
