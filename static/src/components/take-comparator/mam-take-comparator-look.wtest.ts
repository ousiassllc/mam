import { expect } from '@open-wc/testing';
import { sendKeys } from '@web/test-runner-commands';
import { stubFetch } from '../testdata/fakes';
import { loadTailwind } from '../testdata/tailwind';
import { API, body, comparator, dragLane, scored, take } from './testkit';

// The look of the widget against spec/components/lit-widgets-guide.md §2 (W2-W9) and §4, drawn with the real Tailwind CSS of the app.
describe('mam-take-comparator: the look (lit-widgets-guide §4, #1092)', () => {
  let stub: ReturnType<typeof stubFetch>;
  let tailwind: { restore(): void };
  beforeEach(async function () {
    this.timeout(60_000); // the first request waits for the build of the CSS
    tailwind = await loadTailwind();
  });
  afterEach(() => {
    stub.restore();
    tailwind.restore();
  });

  const takes = () => [
    take('t1', 'A', { scores: scored, status: 'ai_pick', status_label: 'AI Pick' }),
    take('t2', 'B', { status: 'selected', status_label: '採用', adopted: true, select_url: undefined }),
    take('t3', 'C', { status: 'rejected', status_label: '除外', select_url: undefined, reject_url: undefined }),
    take('t4', 'D'),
  ];

  it('shows a Status as the Badge of the Shot card: a pill in the colour of the state, no strike-through (TC-2)', async () => {
    stub = stubFetch({ [`GET ${API}`]: body(takes()) });
    const el = await comparator();

    const status = (id: string) => el.querySelector<HTMLElement>(`[data-take="${id}"] [data-status]`)!;
    const style = (id: string) => getComputedStyle(status(id));
    for (const id of ['t1', 't2', 't3', 't4']) {
      expect(parseFloat(style(id).borderTopLeftRadius), `${id} is a pill`).to.be.greaterThan(10);
      expect(parseFloat(style(id).fontSize), `${id} is 12px at least`).to.be.at.least(12);
      expect(style(id).textDecorationLine, `${id} is not struck through`).to.equal('none');
      expect(style(id).borderTopWidth).to.equal('0px');
    }
    // candidate = warning, AI Pick = info, selected = success, rejected = neutral (the ink of the page).
    const colours = ['t1', 't2', 't3', 't4'].map((id) => style(id).color);
    expect(new Set(colours).size, 'the four states differ').to.equal(4);
    expect(status('t3').className).to.contain('text-ink');
    expect(status('t4').className).to.contain('bg-warning/20');
    expect(status('t1').className).to.contain('bg-info/20');
    expect(status('t2').className).to.contain('bg-success/20');

    const adopted = el.querySelector<HTMLElement>('[data-take="t2"] [data-adopted]')!; // 使用中 is a BadgeOK
    expect(parseFloat(getComputedStyle(adopted).borderTopLeftRadius)).to.be.greaterThan(10);
    expect(adopted.className).to.contain('bg-success/20');
  });

  it('right-aligns the scores in digits of one width, under right-aligned column headings (TC-4)', async () => {
    stub = stubFetch({ [`GET ${API}`]: body(takes()) });
    const el = await comparator();

    for (const axis of ['product', 'acting', 'camera', 'continuity']) {
      const cell = el.querySelector<HTMLElement>(`[data-take="t1"] [data-score="${axis}"]`)!;
      const style = getComputedStyle(cell);
      expect(style.textAlign, axis).to.equal('right');
      expect(style.fontFamily, axis).to.match(/mono/i);
      expect(style.fontVariantNumeric, axis).to.contain('tabular-nums');
    }
    const headings = Array.from(el.querySelectorAll<HTMLElement>('[data-scores] thead th')).map(
      (th) => getComputedStyle(th).textAlign,
    );
    expect(headings.slice(1, 5)).to.deep.equal(['right', 'right', 'right', 'right']); // Product … Continuity
    expect(headings[0]).to.not.equal('right'); // Take stays on the left
  });

  it('gives every button and field the focus ring, the buttons a size of 32px (24px for ×) and a ripple (TC-1)', async () => {
    stub = stubFetch({ [`GET ${API}`]: body(takes()) });
    const el = await comparator();
    dragLane(el, 't1', 0, 0.5);
    dragLane(el, 't4', 0.5, 1);
    await Promise.resolve();
    await el.updateComplete;

    const controls = el.querySelectorAll<HTMLElement>('button, input');
    expect(controls.length).to.be.greaterThan(8); // 選択 / 除外 per row, ×, 合成, 選び直す, the words, 言葉で合成
    for (const control of controls) {
      expect(control.classList.contains('focus-visible:focus-ring'), control.outerHTML.slice(0, 90)).to.equal(true);
    }
    for (const action of ['confirm-partial', 'confirm-instruction']) {
      const button = el.querySelector<HTMLElement>(`[data-action="${action}"]`)!;
      expect(button.getBoundingClientRect().height, action).to.be.at.least(32);
      expect(parseFloat(getComputedStyle(button).fontSize), action).to.be.at.least(14); // text-sm
      expect(getComputedStyle(button).fontWeight, action).to.equal('600');
      expect(button.hasAttribute('data-ripple'), action).to.equal(true);
    }
    const remove = el.querySelector<HTMLElement>('[data-action="remove-segment"]')!;
    expect(remove.getBoundingClientRect().width).to.be.at.least(24);
    expect(remove.getBoundingClientRect().height).to.be.at.least(24);
    expect(remove.hasAttribute('data-ripple')).to.equal(true);
    // The buttons of a row are the text of `atoms.LinkButton`: no ripple.
    expect(el.querySelector('[data-action="select"]')!.hasAttribute('data-ripple')).to.equal(false);
  });

  it('shows the focus ring (an outline) on a button of the table that has the focus from the keyboard (TC-1)', async () => {
    stub = stubFetch({ [`GET ${API}`]: body(takes()) });
    const el = await comparator();

    const button = el.querySelector<HTMLButtonElement>('[data-action="select"]')!;
    button.focus();
    await sendKeys({ press: 'Shift' }); // :focus-visible matches after a key press
    expect(button.matches(':focus-visible')).to.equal(true);
    expect(getComputedStyle(button).outlineStyle).to.equal('solid');
  });

  it('draws the selection panel as a card and the numbers of the picked parts at 12px (TC-3, TC-7)', async () => {
    stub = stubFetch({ [`GET ${API}`]: body(takes()) });
    const el = await comparator();
    dragLane(el, 't1', 0, 0.5);
    await el.updateComplete;

    const panel = el.querySelector<HTMLElement>('[data-selection]')!;
    const style = getComputedStyle(panel);
    expect(panel.className).to.contain('bg-surface');
    expect(style.backgroundColor).to.not.equal('rgba(0, 0, 0, 0)');
    expect(parseFloat(style.borderTopLeftRadius)).to.be.greaterThan(0);

    const picked = el.querySelector<HTMLElement>('[data-picked]')!;
    expect(parseFloat(getComputedStyle(picked).fontSize)).to.be.at.least(12);
  });

  it('draws the empty state as a dashed frame (TC-6)', async () => {
    stub = stubFetch({ [`GET ${API}`]: body([]) });
    const el = await comparator();

    const empty = el.querySelector<HTMLElement>('[data-empty]')!;
    const style = getComputedStyle(empty);
    expect(style.borderTopStyle).to.equal('dashed');
    expect(parseFloat(style.paddingTop)).to.equal(24); // p-6
    expect(parseFloat(style.borderTopLeftRadius)).to.be.greaterThan(0);
  });
});
