import { expect, fixture, html, waitUntil } from '@open-wc/testing';
import { stubFetch } from '../testdata/fakes';
import type { MamTakeComparator } from './mam-take-comparator';
import { API, body, comparator, scored, stubConfirm, take, text } from './testkit';

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

    const confirm = stubConfirm(true);
    try {
      (el.querySelector('[data-take="t1"] [data-action="reject"]') as HTMLButtonElement).click();
      await waitUntil(() => stub.requests.some((r) => r.method === 'DELETE'));
    } finally {
      confirm.restore();
    }
    expect(stub.requests.find((r) => r.method === 'DELETE')!.url).to.equal('/takes/t1');
  });

  it('asks before rejecting and sends nothing when the user declines', async () => {
    stub = stubFetch({
      [`GET ${API}`]: body([take('t1', 'A')]),
      'DELETE /takes/t1': { body: '<div id="shot-s1"></div>' },
    });
    const el = await comparator();
    const button = () => el.querySelector('[data-take="t1"] [data-action="reject"]') as HTMLButtonElement;

    const declined = stubConfirm(false);
    try {
      button().click();
      await el.updateComplete;
      expect(declined.messages).to.deep.equal(['この Take を除外しますか？']);
    } finally {
      declined.restore();
    }
    expect(stub.requests.some((r) => r.method === 'DELETE')).to.equal(false);

    const accepted = stubConfirm(true);
    try {
      button().click();
      await waitUntil(() => stub.requests.some((r) => r.method === 'DELETE'));
      expect(accepted.messages).to.deep.equal(['この Take を除外しますか？']);
    } finally {
      accepted.restore();
    }
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
