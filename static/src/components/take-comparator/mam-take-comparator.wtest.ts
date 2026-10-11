import { aTimeout, expect, fixture, html, oneEvent, waitUntil } from '@open-wc/testing';
import './mam-take-comparator';
import type { MamTakeComparator, TakeScore } from './mam-take-comparator';
import { stubFetch, type RecordedRequest } from '../testdata/fakes';

const API = '/api/v1/shots/s1/takes';

function take(id: string, label: string, over: Partial<TakeScore> = {}): TakeScore {
  return {
    id,
    label,
    status: 'candidate',
    adopted: false,
    model_name: 'kling-3.0',
    resolution: '1080p',
    fps: 25,
    duration_seconds: 4,
    scores: null,
    proxied: false,
    video_url: `/takes/${id}/video`,
    thumbnail_url: `/takes/${id}/thumbnail`,
    judge_result_url: `/api/v1/takes/${id}/judge-result`,
    select_url: `/takes/${id}/select`,
    reject_url: `/takes/${id}`,
    partial_usable: true,
    ...over,
  };
}

const scored = { product: 65, acting: 100, camera: 85, continuity: 100, overall: 87.5 };

function body(takes: TakeScore[], partialUrl: string | null = '/shots/s1/takes/partial') {
  return { body: { shot_id: 's1', duration_seconds: 4, partial_url: partialUrl ?? undefined, takes } };
}

async function comparator(): Promise<MamTakeComparator> {
  const el = await fixture<MamTakeComparator>(html`<mam-take-comparator api-url=${API}></mam-take-comparator>`);
  await waitUntil(() => el.querySelector('[data-scores], [data-empty]') !== null, 'the Takes were not shown');
  return el;
}

/** Drags on the lane of a Take from x0 to x1 (as fractions of the lane width). */
function dragLane(el: HTMLElement, takeId: string, x0: number, x1: number): void {
  const track = el.querySelector(`[data-lane="${takeId}"] [data-track]`) as HTMLElement;
  const rect = track.getBoundingClientRect();
  const at = (f: number) => ({
    clientX: rect.left + rect.width * f,
    clientY: rect.top + 3,
    pointerId: 1,
    bubbles: true,
  });
  track.dispatchEvent(new PointerEvent('pointerdown', at(x0)));
  track.dispatchEvent(new PointerEvent('pointermove', at((x0 + x1) / 2)));
  track.dispatchEvent(new PointerEvent('pointermove', at(x1)));
  track.dispatchEvent(new PointerEvent('pointerup', at(x1)));
}

const text = (el: Element, selector: string) => el.querySelector(selector)?.textContent?.trim();

describe('mam-take-comparator: score table', () => {
  let stub: ReturnType<typeof stubFetch>;
  afterEach(() => stub.restore());

  it('lists the Takes with their four scores and status', async () => {
    stub = stubFetch({
      [`GET ${API}`]: body([
        take('t1', 'A', { scores: scored, status: 'ai_pick', verdict: 'fail' }),
        take('t2', 'B', { status: 'selected', adopted: true, select_url: undefined }),
        take('t3', 'C', {
          status: 'rejected',
          video_url: undefined,
          select_url: undefined,
          reject_url: undefined,
          partial_usable: false,
        }),
      ]),
    });
    const el = await comparator();

    const a = el.querySelector('[data-take="t1"]')!;
    expect(text(a, '[data-score="product"]')).to.equal('65');
    expect(text(a, '[data-score="acting"]')).to.equal('100');
    expect(text(a, '[data-score="camera"]')).to.equal('85');
    expect(text(a, '[data-score="continuity"]')).to.equal('100');
    expect(text(a, '[data-status]')).to.equal('AI Pick');
    expect(a.textContent).to.contain('FAIL');

    const b = el.querySelector('[data-take="t2"]')!;
    expect(text(b, '[data-score="product"]')).to.equal('—'); // not judged yet
    expect(text(b, '[data-status]')).to.equal('Selected');
    expect(b.querySelector('[data-adopted]')).to.not.equal(null);
    expect(b.querySelector('[data-action="select"]')).to.equal(null); // already selected

    const c = el.querySelector('[data-take="t3"]')!;
    expect(text(c, '[data-status]')).to.equal('Rejected');
    expect(c.querySelector('[data-action]')).to.equal(null);
    expect(el.querySelector('[data-lane="t3"]')).to.equal(null); // no lane for a rejected Take
    expect(el.querySelectorAll('mam-video-player').length).to.equal(2);
  });

  it('says so when the Shot has no Take', async () => {
    stub = stubFetch({ [`GET ${API}`]: body([]) });
    const el = await comparator();
    expect(el.querySelector('[data-empty]')).to.not.equal(null);
  });

  it('offers an action only when the server gave its link, whatever the status says', async () => {
    stub = stubFetch({
      [`GET ${API}`]: body([
        take('t1', 'A', { select_url: undefined }), // a candidate the server will not let be selected
        take('t2', 'B', { status: 'selected', reject_url: undefined }), // a selected Take the server will not let be rejected
        take('t3', 'C', { select_url: undefined, reject_url: undefined, partial_usable: false }), // a candidate with no action at all
      ]),
    });
    const el = await comparator();

    expect(el.querySelector('[data-take="t1"] [data-action="select"]')).to.equal(null);
    expect(el.querySelector('[data-take="t1"] [data-action="reject"]')).to.not.equal(null);
    expect(el.querySelector('[data-take="t2"] [data-action="select"]')).to.not.equal(null);
    expect(el.querySelector('[data-take="t2"] [data-action="reject"]')).to.equal(null);
    expect(el.querySelector('[data-take="t3"] [data-action]')).to.equal(null);
    expect(el.querySelector('[data-lane="t3"]')).to.equal(null);
  });

  it('hides Partial Take Use when the server gave no partial_url', async () => {
    stub = stubFetch({ [`GET ${API}`]: body([take('t1', 'A'), take('t2', 'B')], null) });
    const el = await comparator();

    expect(el.querySelector('[aria-label="区間指定"]')).to.equal(null);
    expect(el.querySelector('[data-lane]')).to.equal(null);
    expect(el.querySelector('[data-take="t1"] [data-action="select"]')).to.not.equal(null);
  });

  it('shows the server error when the Takes cannot be loaded', async () => {
    stub = stubFetch({ [`GET ${API}`]: { status: 404, body: '<div>対象が見つかりません</div>' } });
    const el = await fixture<MamTakeComparator>(html`<mam-take-comparator api-url=${API}></mam-take-comparator>`);
    await waitUntil(() => el.querySelector('[data-error]') !== null);
    expect(text(el, '[data-error]')).to.equal('対象が見つかりません');
  });
});

describe('mam-take-comparator: choosing a Take', () => {
  let stub: ReturnType<typeof stubFetch>;
  afterEach(() => stub.restore());

  it('selects and rejects through the URLs the server gave, then reloads', async () => {
    let listed = 0;
    stub = stubFetch({
      [`GET ${API}`]: () => {
        listed += 1;
        return body([take('t1', 'A'), take('t2', 'B', listed > 1 ? { status: 'selected', adopted: true } : {})]);
      },
      'POST /takes/t2/select': { body: '<div id="shot-s1"></div>' },
      'DELETE /takes/t1': { body: '<div id="shot-s1"></div>' },
    });
    const el = await comparator();

    (el.querySelector('[data-take="t2"] [data-action="select"]') as HTMLButtonElement).click();
    await waitUntil(() => text(el, '[data-take="t2"] [data-status]') === 'Selected');
    const post = stub.requests.find((r) => r.method === 'POST')!;
    expect(post.url).to.equal('/takes/t2/select');
    expect(post.headers).to.have.property('X-CSRF-Token');

    (el.querySelector('[data-take="t1"] [data-action="reject"]') as HTMLButtonElement).click();
    await waitUntil(() => stub.requests.some((r) => r.method === 'DELETE'));
    expect(stub.requests.find((r) => r.method === 'DELETE')!.url).to.equal('/takes/t1');
  });

  it('shows why a choice failed and keeps the table', async () => {
    stub = stubFetch({
      [`GET ${API}`]: body([take('t1', 'A')]),
      'POST /takes/t1/select': {
        status: 422,
        body: '<div data-toast>この Take のファイルはアーカイブされているため選択できません</div>',
      },
    });
    const el = await comparator();

    (el.querySelector('[data-action="select"]') as HTMLButtonElement).click();
    await waitUntil(() => el.querySelector('[data-error]') !== null);

    expect(text(el, '[data-error]')).to.contain('アーカイブされている');
    expect(el.querySelector('[data-scores]')).to.not.equal(null);
  });
});

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
});
