import { expect, waitUntil } from '@open-wc/testing';
import { expectFocusOn } from '../focus-restore/focus-assert';
import { stubFetch } from '../testdata/fakes';
import { API, editor, order, text, three, timeline } from './timeline-fixtures';

/** Focuses a control and presses it, as a keyboard user does. */
function press(el: Element, selector: string): void {
  const button = el.querySelector(selector) as HTMLButtonElement;
  button.focus();
  button.click();
}

describe('mam-timeline-editor: focus (spec §5.2)', () => {
  let stub: ReturnType<typeof stubFetch>;
  afterEach(() => stub.restore());

  it('keeps the focus on the "↑" of the clip that was moved up, and says where it went', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();

    press(el, '[data-clip="c"] [data-action="up"]'); // a c b
    await waitUntil(() => order(el).join() === 'a,c,b');

    expectFocusOn(el.querySelector('[data-clip="c"] [data-action="up"]'));
    expect(text(el, '[data-announce]')).to.equal('Scene 1 · c を 2 番目へ移動しました');
    expect(el.querySelector('[data-announce]')?.getAttribute('role')).to.equal('status');
  });

  it('keeps the row with its clip, so that the buttons still name the clip that was moved', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();
    const row = el.querySelector('[data-clip="b"]');

    press(el, '[data-clip="b"] [data-action="down"]'); // a c b
    await waitUntil(() => order(el).join() === 'a,c,b');

    expect(el.querySelector('[data-clip="b"]') === row, 'the row of b moved with it').to.equal(true); // keyed: the row moved with its clip
    // b is the last now and "↓" is disabled: the focus is on its "↑" (a disabled button cannot take it).
    expectFocusOn(el.querySelector('[data-clip="b"] [data-action="up"]'));
  });

  it('moves the focus to the other arrow of a clip that reached the top', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();

    press(el, '[data-clip="b"] [data-action="up"]'); // b a c
    await waitUntil(() => order(el).join() === 'b,a,c');

    expectFocusOn(el.querySelector('[data-clip="b"] [data-action="down"]'));
  });

  it('puts the focus on the "外す" of the clip that took the place of a removed one, then on the last row', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();

    press(el, '[data-clip="a"] [data-action="remove"]');
    await waitUntil(() => order(el).join() === 'b,c');
    expectFocusOn(el.querySelector('[data-clip="b"] [data-action="remove"]'));
    expect(text(el, '[data-announce]')).to.equal('Scene 1 · a を外しました（2 クリップ）');

    press(el, '[data-clip="c"] [data-action="remove"]'); // the last one of the list
    await waitUntil(() => order(el).join() === 'b');
    // One clip is left: nothing can be pressed on its row any more (外す, ↑, ↓ are disabled), so the row.
    expectFocusOn(el.querySelector('[data-clip="b"]'));
  });

  it('puts the focus on the notice once the save made the Save button disabled', async () => {
    let gets = 0;
    stub = stubFetch({
      [`GET ${API}`]: () => ({ body: timeline(`e${++gets}`, three(), { version_number: gets }) }),
      'PUT /edit-timelines/e1': { status: 200 },
    });
    const el = await editor();
    press(el, '[data-clip="b"] [data-action="up"]');
    await waitUntil(() => order(el).join() === 'b,a,c');

    press(el, '[data-action="save"]');
    await waitUntil(() => text(el, '[data-notice]') === '保存しました');

    expect(el.querySelector('[data-action="save"]')?.hasAttribute('disabled')).to.equal(true);
    expectFocusOn(el.querySelector('[data-notice]'));
  });

  it('keeps the focus on the Save button when the save failed and it can be pressed again', async () => {
    stub = stubFetch({
      [`GET ${API}`]: { body: timeline('e1', three()) },
      'PUT /edit-timelines/e1': { status: 422, body: { title: 'refused', detail: 'refused' } },
    });
    const el = await editor();
    press(el, '[data-clip="b"] [data-action="up"]');
    await waitUntil(() => order(el).join() === 'b,a,c');

    press(el, '[data-action="save"]');
    await waitUntil(() => el.querySelector('[data-error]') !== null);

    expectFocusOn(el.querySelector('[data-action="save"]'));
  });

  it('puts the focus on the list once "変更を戻す" has disabled itself', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();
    press(el, '[data-clip="b"] [data-action="up"]');
    await waitUntil(() => order(el).join() === 'b,a,c');

    press(el, '[data-action="revert"]');
    await waitUntil(() => order(el).join() === 'a,b,c');

    expectFocusOn(el.querySelector('[data-clips]'));
  });

  it('does not take the focus from a control the user went to while the save was running', async () => {
    stub = stubFetch({
      [`GET ${API}`]: { body: timeline('e1', three()) },
      'PUT /edit-timelines/e1': { status: 200 },
    });
    const el = await editor();
    press(el, '[data-clip="b"] [data-action="up"]');
    await waitUntil(() => order(el).join() === 'b,a,c');

    press(el, '[data-action="save"]');
    const elsewhere = document.createElement('input');
    el.after(elsewhere);
    elsewhere.focus();
    await waitUntil(() => text(el, '[data-notice]') === '保存しました');
    await el.updateComplete;

    expectFocusOn(elsewhere);
    elsewhere.remove();
  });
});
