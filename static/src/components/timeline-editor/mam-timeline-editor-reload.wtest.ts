import { expect, waitUntil } from '@open-wc/testing';
import { stubFetch } from '../testdata/fakes';
import { API, alerts, clip, editor, installChrome, order, three, timeline } from './timeline-fixtures';

describe('mam-timeline-editor: timeline-reload', () => {
  let stub: ReturnType<typeof stubFetch>;
  let chrome: ReturnType<typeof installChrome>;
  beforeEach(() => (chrome = installChrome()));
  afterEach(() => {
    stub.restore();
    chrome.restore();
  });

  it('keeps the unsaved edits on timeline-reload while the server still has the same version, and takes its readonly', async () => {
    let readonly = false;
    stub = stubFetch({ [`GET ${API}`]: () => ({ body: timeline('e1', three(), { readonly }) }) });
    const el = await editor();
    (el.querySelector('[data-clip="c"] [data-action="up"]') as HTMLButtonElement).click();
    await waitUntil(() => order(el).join() === 'a,c,b');
    expect(el.textContent).to.contain('未保存の変更あり');

    readonly = true; // Editor Judge / Final QC / an additional Shot request moved the Project on
    document.dispatchEvent(new CustomEvent('timeline-reload'));
    await waitUntil(() => el.querySelector('[data-readonly]') !== null);
    expect(order(el)).to.deep.equal(['a', 'c', 'b']);
    expect(el.textContent).to.contain('未保存の変更あり');
    expect(alerts()).to.deep.equal([]);
  });

  it('drops the unsaved edits, and says so, when timeline-reload brings a newer version', async () => {
    let newer = false;
    stub = stubFetch({
      [`GET ${API}`]: () => ({
        body: newer
          ? timeline('e2', [clip('x', 0, 4), clip('y', 0, 3)], { version_number: 2 })
          : timeline('e1', three()),
      }),
    });
    const el = await editor();
    (el.querySelector('[data-clip="c"] [data-action="up"]') as HTMLButtonElement).click();
    await waitUntil(() => order(el).join() === 'a,c,b');

    newer = true; // a Rough Cut was made again
    document.dispatchEvent(new CustomEvent('timeline-reload'));
    await waitUntil(() => order(el).join() === 'x,y');
    // A warning alert (not a success-coloured note): what was lost has to be read again.
    expect(alerts()).to.have.length(1);
    expect(alerts()[0]!.kind).to.equal('warning');
    expect(alerts()[0]!.message).to.equal('最新の版に差し替わったため、未保存の変更は破棄されました');
    expect(el.textContent).to.not.contain('未保存の変更あり');
  });

  it('reloads without a notice on timeline-reload when there are no unsaved edits', async () => {
    let newer = false;
    stub = stubFetch({
      [`GET ${API}`]: () => ({
        body: newer ? timeline('e2', [clip('x', 0, 4)], { version_number: 2 }) : timeline('e1', three()),
      }),
    });
    const el = await editor();
    newer = true;
    document.dispatchEvent(new CustomEvent('timeline-reload'));
    await waitUntil(() => order(el).join() === 'x');
    expect(alerts()).to.deep.equal([]);
  });
});
