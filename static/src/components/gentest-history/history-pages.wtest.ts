import { expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// #1079: the history of the generation test is paged. The navigation is a <nav> of links (the page that is shown is a mark with aria-current, the steps
// that have nowhere to go are not links), a link reads its page into the history and puts its address into the address of the browser. The pages are what
// the handler renders (backend/internal/web/gentest_live_fixture_test.go); web-test-runner.config.mjs answers "?page=2".
const TESTDATA = '/src/components/testdata/';

describe('the history of the generation test is paged (#1079)', () => {
  let host: HTMLElement;
  const startUrl = location.href;

  const nav = () => host.querySelector<HTMLElement>('nav[aria-label="ページ移動"]')!;
  const current = () => nav().querySelector<HTMLElement>('[aria-current="page"]')!;
  const link = (id: string) => nav().querySelector<HTMLAnchorElement>(`#gt-pager-${id}`)!;

  async function show(fixture: string): Promise<void> {
    host = document.createElement('div');
    host.innerHTML = await (await fetch(`${TESTDATA}${fixture}`)).text();
    document.body.append(host);
    htmx.process(host);
  }

  afterEach(() => {
    host.remove();
    history.replaceState(null, '', startUrl);
  });

  it('marks the page that is shown, and only the pages that can be gone to are links the keyboard reaches', async () => {
    await show('gentest-history-page1.html');

    expect(current().textContent).to.equal('1');
    expect(current().tagName, 'the current page is not a link').to.equal('SPAN');
    expect(nav().querySelectorAll('[aria-current]').length).to.equal(1);
    expect(nav().textContent).to.contain('60 件中 1–24 件（1 / 3 ページ）');
    const reachable = [...nav().querySelectorAll<HTMLElement>('a[href]')].map((a) => a.textContent);
    expect(reachable).to.deep.equal(['2', '3', '次へ', '最後']);
    for (const a of nav().querySelectorAll<HTMLAnchorElement>('a[href]')) {
      a.focus();
      expect(document.activeElement, `${a.textContent} takes the focus`).to.equal(a);
    }
    const back = [...nav().querySelectorAll<HTMLElement>('span[aria-disabled="true"]')];
    expect(back.map((s) => s.textContent)).to.deep.equal(['最初', '前へ']);
    expect(
      back.every((s) => s.tabIndex === -1),
      'a step with nowhere to go is not reached by Tab',
    ).to.equal(true);
  });

  it('reads the page of a link into the list and puts its address into the history of the browser', async () => {
    await show('gentest-history-page1.html');
    const section = host.querySelector('#gt-history')!;

    link('next').click();
    await waitUntil(() => nav().textContent!.includes('25–48 件'), 'the second page did not come', { timeout: 3000 });

    expect(host.querySelector('#gt-history'), 'the list is swapped whole').to.not.equal(section);
    expect(current().textContent).to.equal('2');
    expect(location.search).to.equal('?page=2');
    expect(link('prev').getAttribute('href')).to.equal('/generation-test/history');
  });
});
