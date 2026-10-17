import { expect, waitUntil } from '@open-wc/testing';
import { expectFocusOn } from '../focus-restore/focus-assert';
import { stubFetch } from '../testdata/fakes';
import { API, body, comparator, dragLane, stubConfirm, take } from './testkit';

const focused = () => document.activeElement;

function press(el: Element, selector: string): void {
  const button = el.querySelector(selector) as HTMLButtonElement;
  button.focus();
  button.click();
}

describe('mam-take-comparator: focus (spec §5.2)', () => {
  let stub: ReturnType<typeof stubFetch>;
  let confirm: ReturnType<typeof stubConfirm>;
  afterEach(() => {
    stub.restore();
    confirm?.restore();
  });

  const candidates = () => [take('t1', 'A'), take('t2', 'B')];
  const afterSelect = () => [
    take('t1', 'A', { status: 'selected', status_label: '採用', adopted: true, select_url: undefined }),
    take('t2', 'B'),
  ];

  it('puts the focus on the "除外" of the same row when "選択" is gone after the Take was selected', async () => {
    let gets = 0;
    stub = stubFetch({
      [`GET ${API}`]: () => body(++gets === 1 ? candidates() : afterSelect()),
      'POST /takes/t1/select': { body: '' },
    });
    const el = await comparator();

    press(el, '[data-take="t1"] [data-action="select"]');
    await waitUntil(() => el.querySelector('[data-take="t1"] [data-action="select"]') === null);

    expectFocusOn(el.querySelector('[data-take="t1"] [data-action="reject"]'));
  });

  it('puts the focus on the "選択" of the same row when the rejected Take is still there', async () => {
    confirm = stubConfirm(true);
    let gets = 0;
    stub = stubFetch({
      [`GET ${API}`]: () =>
        body(
          ++gets === 1
            ? candidates()
            : [
                take('t1', 'A'),
                take('t2', 'B', {
                  status: 'rejected',
                  status_label: '除外',
                  select_url: undefined,
                  reject_url: undefined,
                }),
              ],
        ),
      'DELETE /takes/t2': { body: '' },
    });
    const el = await comparator();

    press(el, '[data-take="t2"] [data-action="reject"]');
    await waitUntil(() => el.querySelector('[data-take="t2"] [data-action="reject"]') === null);

    // Nothing is left to press on the row of the rejected Take: its heading.
    expectFocusOn(el.querySelector('[data-take="t2"] th[scope="row"]'));
  });

  it('puts the focus on the selection panel once the merge cleared the selection and disabled the button', async () => {
    stub = stubFetch({
      [`GET ${API}`]: body(candidates()),
      'POST /shots/s1/takes/partial': { body: '<div id="shot-s1"></div>' },
    });
    const el = await comparator();
    dragLane(el, 't2', 0.5, 1);
    dragLane(el, 't1', 0, 0.5);
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 2);

    press(el, '[data-action="confirm-partial"]');
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 0);
    await el.updateComplete;

    expectFocusOn(el.querySelector('[data-selection]'));
  });

  it('puts the focus on the "×" of the next part when a part was taken out', async () => {
    stub = stubFetch({ [`GET ${API}`]: body(candidates()) });
    const el = await comparator();
    dragLane(el, 't1', 0, 0.5);
    dragLane(el, 't2', 0.5, 1);
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 2);

    press(el, '[data-segment]:nth-child(1) button');
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 1);

    expectFocusOn(el.querySelector('[data-segment] button'));
    expect(focused()?.getAttribute('aria-label')).to.equal('区間 1 を外す');
  });
});
