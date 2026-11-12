import { expect, waitUntil } from '@open-wc/testing';
import { stubFetch } from '../testdata/fakes';
import { API, alerts, editor, installChrome, order, three, timeline } from './timeline-fixtures';

// The first load fails with something other than a 404: the alert says why, and the editor offers the way on (#1091, TL-9's empty state is for 404 only).
describe('<mam-timeline-editor> retry of a failed first load', () => {
  let stub: ReturnType<typeof stubFetch>;
  let chrome: ReturnType<typeof installChrome>;
  beforeEach(() => (chrome = installChrome()));
  afterEach(() => {
    stub.restore();
    chrome.restore();
  });

  it('offers "もう一度読み込む" when the first load fails, keeps it while the load fails, and shows the timeline once it works', async () => {
    let fail = true;
    stub = stubFetch({
      [`GET ${API}`]: () =>
        fail ? { status: 503, body: { title: 'Service Unavailable' } } : { body: timeline('e1', three()) },
    });
    const el = await editor();
    const retry = () => el.querySelector('[data-action="retry"]') as HTMLButtonElement | null;
    expect(el.querySelector('[data-load-failed]')).to.not.equal(null);
    expect(retry()?.textContent).to.contain('もう一度読み込む');
    expect(el.querySelector('[data-empty]')).to.equal(null);
    expect(alerts()).to.have.length(1);

    retry()!.focus();
    retry()!.click(); // still failing
    await waitUntil(() => stub.requests.filter((r) => r.method === 'GET').length === 2);
    await waitUntil(() => el.querySelector('[role="status"]') === null);
    expect(retry()).to.not.equal(null);
    expect(el.querySelector('[data-clips]')).to.equal(null);
    expect(document.activeElement).to.equal(retry());
    expect(alerts()).to.have.length(1); // the same sentence is not piled up

    fail = false;
    retry()!.click();
    await waitUntil(() => order(el).join() === 'a,b,c');
    expect(el.querySelector('[data-load-failed]')).to.equal(null);
    expect(alerts()).to.have.length(1);
  });

  it('shows the empty state, not the retry, when the retry finds there is no Rough Cut (404)', async () => {
    let status = 503;
    stub = stubFetch({ [`GET ${API}`]: () => ({ status, body: { title: 'x' } }) });
    const el = await editor();
    status = 404;
    (el.querySelector('[data-action="retry"]') as HTMLButtonElement).click();
    await waitUntil(() => el.querySelector('[data-empty]') !== null);
    expect(el.querySelector('[data-load-failed]')).to.equal(null);
  });
});
