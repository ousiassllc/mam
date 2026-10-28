import { expect } from '@open-wc/testing';
import { sendKeys } from '@web/test-runner-commands';
import '../../main';

describe('"?" help (spec/components/shared-ui-parts.md §9)', () => {
  let host: HTMLElement;
  const toggle = (n = 1) => host.querySelector<HTMLButtonElement>(`#t${n}`)!;
  const panel = (n = 1) => host.querySelector<HTMLElement>(`#p${n}`)!;

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = `
      <section>
        <h2>見出し 1</h2>
        <button type="button" id="t1" data-help-toggle aria-expanded="false" aria-controls="p1" aria-label="見出し 1の説明">?</button>
        <div id="p1" data-help-panel hidden><p>説明 1</p><a id="link1" href="#x">リンク</a></div>
      </section>
      <section>
        <button type="button" id="t2" data-help-toggle aria-expanded="false" aria-controls="p2" aria-label="見出し 2の説明">?</button>
        <div id="p2" data-help-panel hidden><p>説明 2</p></div>
      </section>`;
    document.body.append(host);
  });
  afterEach(() => host.remove());

  it('is closed at first and opens on click, telling aria-expanded', () => {
    expect(panel().hidden).to.equal(true);
    expect(toggle().getAttribute('aria-expanded')).to.equal('false');

    toggle().click();

    expect(panel().hidden).to.equal(false);
    expect(toggle().getAttribute('aria-expanded')).to.equal('true');
  });

  it('closes on a second click', () => {
    toggle().click();
    toggle().click();

    expect(panel().hidden).to.equal(true);
    expect(toggle().getAttribute('aria-expanded')).to.equal('false');
  });

  it('opens and closes from the keyboard and keeps the focus on the button', async () => {
    toggle().focus();

    await sendKeys({ press: 'Enter' });
    expect(panel().hidden).to.equal(false);
    expect(document.activeElement === toggle()).to.equal(true);

    await sendKeys({ press: 'Space' });
    expect(panel().hidden).to.equal(true);
    expect(document.activeElement === toggle()).to.equal(true);
  });

  it('closes on Esc from the button', async () => {
    toggle().click();
    toggle().focus();

    await sendKeys({ press: 'Escape' });

    expect(panel().hidden).to.equal(true);
    expect(toggle().getAttribute('aria-expanded')).to.equal('false');
    expect(document.activeElement === toggle()).to.equal(true);
  });

  it('closes on Esc from inside the panel and returns the focus to the button', async () => {
    toggle().click();
    host.querySelector<HTMLElement>('#link1')!.focus();

    await sendKeys({ press: 'Escape' });

    expect(panel().hidden).to.equal(true);
    expect(document.activeElement === toggle()).to.equal(true);
  });

  it('leaves Esc alone when no help is open', async () => {
    const other = document.createElement('button');
    host.append(other);
    other.focus();
    let prevented = false;
    document.addEventListener('keydown', (e) => (prevented = e.defaultPrevented), { once: true });

    await sendKeys({ press: 'Escape' });

    expect(prevented).to.equal(false);
    expect(document.activeElement === other).to.equal(true);
  });

  it('does not close a help when Esc is pressed elsewhere in the page', async () => {
    toggle().click();
    toggle(2).focus();

    await sendKeys({ press: 'Escape' });

    expect(panel().hidden).to.equal(false);
  });

  it('keeps helps independent', () => {
    toggle().click();
    toggle(2).click();

    expect(panel().hidden).to.equal(false);
    expect(panel(2).hidden).to.equal(false);
  });
});
