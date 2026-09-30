import { expect } from '@open-wc/testing';
import { createConnectionNotice } from './connection-notice';

describe('connection notice', () => {
  afterEach(() => document.getElementById('connection-lost-region')?.remove());

  const notices = () => document.querySelectorAll<HTMLElement>('[data-connection-lost]');

  it('tells the user what stopped updating and lets them reconnect', () => {
    let retries = 0;
    const notice = createConnectionNotice('チャット');
    notice.show(() => (retries += 1));
    notice.show(() => (retries += 1)); // shown once

    expect(notices().length).to.equal(1);
    expect(notices()[0]!.getAttribute('role')).to.equal('alert');
    expect(notices()[0]!.textContent).to.contain('接続が切れました');
    expect(notices()[0]!.textContent).to.contain('チャット');
    (notices()[0]!.querySelector('button') as HTMLButtonElement).click();
    expect(retries).to.equal(1);
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
});
