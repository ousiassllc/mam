import { expect } from '@open-wc/testing';
import '../../main';

describe('side menu fold button (spec/components/side-menu.md §4)', () => {
  let host: HTMLElement;
  const nav = () => host.querySelector<HTMLElement>('nav')!;
  const button = () => host.querySelector<HTMLButtonElement>('[data-sidenav-toggle]')!;
  const stored = () => document.cookie.split('; ').find((c) => c.startsWith('mam_nav_test='));

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = `
      <nav id="sidenav" aria-label="メニュー" data-folded="false" data-cookie="mam_nav_test" data-cookie-folded="folded"
           data-label-fold="メニューをたたむ" data-label-open="メニューを開く">
        <button type="button" data-sidenav-toggle aria-controls="sidenav" aria-expanded="true" aria-label="メニューをたたむ" title="メニューをたたむ"></button>
        <a href="/">プロジェクト</a>
      </nav>`;
    document.body.append(host);
  });
  afterEach(() => {
    // biome-ignore lint/suspicious/noDocumentCookie: removes the cookie of the test
    document.cookie = 'mam_nav_test=; Path=/; Max-Age=0';
    host.remove();
  });

  it('folds the menu: the state, the button name and the cookie that the server reads', () => {
    button().click();

    expect(nav().dataset.folded).to.equal('true');
    expect(button().getAttribute('aria-expanded')).to.equal('false');
    expect(button().getAttribute('aria-label')).to.equal('メニューを開く');
    expect(button().getAttribute('title')).to.equal('メニューを開く');
    expect(stored()).to.equal('mam_nav_test=folded');
  });

  it('opens it again and stores that', () => {
    button().click();
    button().click();

    expect(nav().dataset.folded).to.equal('false');
    expect(button().getAttribute('aria-expanded')).to.equal('true');
    expect(button().getAttribute('aria-label')).to.equal('メニューをたたむ');
    expect(stored()).to.equal('mam_nav_test=open');
  });

  it('is a button: the keyboard (Enter, Space) clicks it, and it keeps the focus', () => {
    button().focus();
    button().click();

    expect(document.activeElement).to.equal(button());
  });
});
