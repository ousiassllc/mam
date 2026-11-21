import { expect, waitUntil } from '@open-wc/testing';
import { createConnectionNotice } from './connection-notice';
import { installConnectionNoticeTemplate } from '../testdata/fakes';

describe('connection notice', () => {
  let removeTemplate: () => void;
  beforeEach(() => (removeTemplate = installConnectionNoticeTemplate()));
  afterEach(() => removeTemplate());

  const notices = () => document.querySelectorAll<HTMLElement>('[data-connection-lost]:not([data-leaving])');

  it('tells the user what stopped updating and lets them reconnect', () => {
    let retries = 0;
    const notice = createConnectionNotice('チャット');
    notice.show(() => (retries += 1));
    notice.show(() => (retries += 1)); // shown once

    expect(notices().length).to.equal(1);
    expect(notices()[0]!.getAttribute('role')).to.equal('alert');
    expect(notices()[0]!.textContent).to.contain('接続が切れました');
    expect(notices()[0]!.textContent).to.contain('チャット');
    (notices()[0]!.querySelector('[data-action="reconnect"]') as HTMLButtonElement).click();
    expect(retries).to.equal(1);
  });

  it('is a clone of the template: the markup is not built here', () => {
    const template = document.getElementById('connection-notice-template') as HTMLTemplateElement;
    const notice = createConnectionNotice('チャット');
    notice.show(() => {});
    const shown = notices()[0]!.cloneNode(true) as HTMLElement;
    shown.dataset.connectionLost = '';
    shown.querySelector('[data-connection-lost-message]')!.textContent = '';
    expect(shown.outerHTML).to.equal(template.content.firstElementChild!.outerHTML);
  });

  it('shows nothing when the page has no template', () => {
    removeTemplate();
    createConnectionNotice('チャット').show(() => {});
    expect(notices().length).to.equal(0);
    removeTemplate = installConnectionNoticeTemplate();
  });

  it('goes away when the connection is back, and keeps one notice per connection', () => {
    const chat = createConnectionNotice('チャット');
    const progress = createConnectionNotice('生成の進捗');
    chat.show(() => {});
    progress.show(() => {});
    expect(notices().length).to.equal(2);
    chat.hide();
    expect(notices().length).to.equal(1);
    expect(notices()[0]!.dataset.connectionLost).to.equal('生成の進捗');
    chat.show(() => {});
    expect(notices().length).to.equal(2);
  });

  it('fades out when the connection is back: it stays until the transition is over (M5)', async () => {
    const style = document.createElement('style');
    style.textContent =
      '[data-connection-lost]{opacity:1;transition:opacity 200ms linear}[data-connection-lost][data-leaving]{opacity:0}';
    document.head.append(style);
    try {
      const notice = createConnectionNotice('チャット');
      notice.show(() => {});
      const shown = document.querySelector<HTMLElement>('[data-connection-lost]')!;
      expect(getComputedStyle(shown).opacity).to.equal('1'); // the style the fade starts from

      notice.hide();

      expect(shown.hasAttribute('data-leaving')).to.equal(true);
      expect(shown.isConnected).to.equal(true);
      expect(notices().length).to.equal(0);
      await waitUntil(() => !shown.isConnected, 'the notice was not removed after the fade');
    } finally {
      style.remove();
    }
  });
});
