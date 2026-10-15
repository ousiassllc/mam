import { aTimeout, expect, oneEvent, waitUntil } from '@open-wc/testing';
import { snapToFrame, DEFAULT_FPS } from '../lib/video';
import { stubFetch, type RecordedRequest } from '../testdata/fakes';
import { API, body, comparator, dragLane, take, text } from './testkit';

describe('mam-take-comparator: Partial Take Use', () => {
  let stub: ReturnType<typeof stubFetch>;
  afterEach(() => stub.restore());

  const takes = () => [take('t1', 'A'), take('t2', 'B')];

  it('picks parts by dragging on the timeline and joins them in the order picked', async () => {
    let posted: RecordedRequest | undefined;
    stub = stubFetch({
      [`GET ${API}`]: body(takes()),
      'POST /shots/s1/takes/partial': (req) => {
        posted = req;
        return { body: '<div id="shot-s1"></div>' };
      },
    });
    const el = await comparator();
    const confirm = () => el.querySelector('[data-action="confirm-partial"]') as HTMLButtonElement;
    expect(confirm().disabled).to.equal(true);

    dragLane(el, 't2', 0.5, 1); // Take B, 2-4 s
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 1);
    expect(confirm().disabled).to.equal(true); // one part is not a composition
    dragLane(el, 't1', 0, 0.5); // Take A, 0-2 s
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 2);

    const parts = Array.from(el.querySelectorAll('[data-segment] span')).map((s) => s.textContent);
    expect(parts).to.deep.equal(['Take B の 2.0-4.0秒', 'Take A の 0.0-2.0秒']);
    expect(el.textContent).to.contain('合計 4.0 秒');
    expect(el.querySelectorAll('[data-picked]').length).to.equal(2);

    const confirmed = oneEvent(el, 'partial-take-confirmed');
    confirm().click();
    const { detail } = (await confirmed) as CustomEvent<{
      segments: { take_id: string; start: number; end: number }[];
    }>;

    expect(posted!.body).to.deep.equal({
      segments: [
        { take_id: 't2', start: 2, end: 4 },
        { take_id: 't1', start: 0, end: 2 },
      ],
    });
    expect(detail.segments.map((s) => s.take_id)).to.deep.equal(['t2', 't1']);
    expect(el.querySelectorAll('[data-segment]').length).to.equal(0); // the selection is cleared
  });

  it('ignores a drag that is too short and removes a part on request', async () => {
    stub = stubFetch({ [`GET ${API}`]: body(takes()) });
    const el = await comparator();

    dragLane(el, 't1', 0.2, 0.22); // 0.08 s
    await aTimeout(30);
    expect(el.querySelectorAll('[data-segment]').length).to.equal(0);
    expect(text(el, '[data-error]')).to.contain('0.3 秒以上');

    dragLane(el, 't1', 0, 0.5);
    dragLane(el, 't2', 0.5, 1);
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 2);
    (el.querySelector('[data-segment] [data-action="remove-segment"]') as HTMLButtonElement).click();
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 1);
    expect(text(el, '[data-segment] span')).to.equal('Take B の 2.0-4.0秒');
  });

  it('applies the limits the server sends, not values of its own', async () => {
    stub = stubFetch({
      [`GET ${API}`]: body(takes(), '/shots/s1/takes/partial', {
        max_segments: 2,
        min_segment_seconds: 1,
        min_axis_score: 60,
      }),
    });
    const el = await comparator();

    dragLane(el, 't1', 0.1, 0.3); // 0.8 s: long enough for 0.3 s, too short for 1 s
    await aTimeout(30);
    expect(el.querySelectorAll('[data-segment]').length).to.equal(0);
    expect(text(el, '[data-error]')).to.contain('1 秒以上');

    dragLane(el, 't1', 0, 0.5);
    dragLane(el, 't2', 0.5, 1);
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 2);
    dragLane(el, 't1', 0, 0.5);
    await aTimeout(30);
    expect(el.querySelectorAll('[data-segment]').length).to.equal(2);
    expect(text(el, '[data-error]')).to.contain('最大 2 個');
  });

  it('sends words to the server as they are', async () => {
    let posted: RecordedRequest | undefined;
    stub = stubFetch({
      [`GET ${API}`]: body(takes()),
      'POST /shots/s1/takes/partial': (req) => {
        posted = req;
        return { body: '<div id="shot-s1"></div>' };
      },
    });
    const el = await comparator();
    const button = el.querySelector('[data-action="confirm-instruction"]') as HTMLButtonElement;
    expect(button.disabled).to.equal(true);

    const input = el.querySelector('input[name="instruction"]') as HTMLInputElement;
    input.value = 'Take A の 0-2秒 + Take B の 2-4秒';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    await waitUntil(() => !button.disabled);
    const confirmed = oneEvent(el, 'partial-take-confirmed');
    button.click();
    await confirmed;

    expect(posted!.body).to.deep.equal({ instruction: 'Take A の 0-2秒 + Take B の 2-4秒' });
    await waitUntil(() => input.value === '');
  });

  it('reports a refusal of the server and keeps the picked parts', async () => {
    stub = stubFetch({
      [`GET ${API}`]: body(takes()),
      'POST /shots/s1/takes/partial': { status: 503, body: '<div data-toast>生成機能が有効になっていません</div>' },
    });
    const el = await comparator();
    dragLane(el, 't1', 0, 0.5);
    dragLane(el, 't2', 0.5, 1);
    await waitUntil(() => el.querySelectorAll('[data-segment]').length === 2);

    (el.querySelector('[data-action="confirm-partial"]') as HTMLButtonElement).click();
    await waitUntil(() => el.querySelector('[data-error]') !== null);

    expect(text(el, '[data-error]')).to.contain('有効になっていません');
    expect(el.querySelectorAll('[data-segment]').length).to.equal(2);
  });

  it('snaps a drag on a Take that reports no frame rate to the default frame rate', async () => {
    stub = stubFetch({ [`GET ${API}`]: body([take('t1', 'A', { fps: undefined })]) });
    const el = await comparator();

    dragLane(el, 't1', 0.1234, 0.6); // 0.4936 s: a different frame at 25 fps than at DEFAULT_FPS
    await waitUntil(() => el.querySelectorAll('[data-picked]').length === 1);

    const left = parseFloat((el.querySelector('[data-picked]') as HTMLElement).style.left);
    expect(left).to.be.closeTo((snapToFrame(0.4936, DEFAULT_FPS) / 4) * 100, 0.01);
  });
});
