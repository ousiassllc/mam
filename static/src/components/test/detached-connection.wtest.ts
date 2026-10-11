import { aTimeout, expect } from '@open-wc/testing';
import { initChatWorkspaces } from '../../chat';
import { initShotProgress } from '../../shot';
import { MAX_RETRIES } from '../lib/ws';
import { FakeSocket, installConnectionNoticeTemplate, stubWebSocket } from '../testdata/fakes';

// A connection whose owner left the document must be closed: otherwise the resume listeners
// (online / visibilitychange) reopen a socket for something that is gone.
describe('connections of elements that left the document', () => {
  let restoreSocket: () => void;
  let removeTemplate: () => void;
  const setTimeoutOriginal = window.setTimeout;
  let host: HTMLElement;

  beforeEach(() => {
    restoreSocket = stubWebSocket();
    removeTemplate = installConnectionNoticeTemplate();
    // The backoff between attempts (500 ms and more) would make the tests take minutes: run it at once.
    window.setTimeout = ((fn: () => void, ms?: number, ...args: unknown[]) =>
      (ms ?? 0) >= 400 ? setTimeoutOriginal(fn, 0) : setTimeoutOriginal(fn, ms, ...args)) as typeof window.setTimeout;
    host = document.createElement('div');
    document.body.append(host);
  });

  afterEach(() => {
    window.setTimeout = setTimeoutOriginal;
    host.remove();
    removeTemplate();
    restoreSocket();
  });

  /** Fails the open sockets one attempt at a time; `beforeLast` runs just before the attempt that gives up. */
  async function failUntilGiveUp(beforeLast: () => void): Promise<void> {
    for (let attempt = 1; attempt <= MAX_RETRIES; attempt++) {
      if (attempt === MAX_RETRIES) beforeLast();
      FakeSocket.instances.filter((s) => !s.closed).forEach((s) => ((s.closed = true), s.onclose?.()));
      await aTimeout(20);
    }
  }

  async function expectNoReconnect(): Promise<void> {
    const before = FakeSocket.instances.length;
    window.dispatchEvent(new Event('online'));
    await aTimeout(20);
    expect(FakeSocket.instances.length).to.equal(before);
    expect(document.querySelector('[data-connection-lost]')).to.equal(null);
  }

  it('stops following a Shot card that was replaced before the socket gave up', async () => {
    host.innerHTML = '<article id="card" data-shot-progress data-ws-url="/ws/shots/s1" data-refresh-url="/refresh/s1">生成中</article>';
    initShotProgress(host);
    expect(FakeSocket.instances.length).to.equal(1);

    await failUntilGiveUp(() => host.replaceChildren());

    await expectNoReconnect();
  });

  it('stops following a Chat Workspace that left the page', async () => {
    host.innerHTML = `<section id="chat-workspace" data-messages-url="/projects/p1/messages" data-ws-url="/ws/projects/p1">
      <div id="chat-thread"></div>
      <form id="chat-composer"><textarea id="composer-content"></textarea><input id="composer-files" type="file" /></form>
    </section>`;
    initChatWorkspaces(host);
    expect(FakeSocket.instances.length).to.equal(1);

    await failUntilGiveUp(() => host.replaceChildren());

    await expectNoReconnect();
  });

  it('closes the socket of a Chat Workspace that left the page when a message arrives', async () => {
    host.innerHTML = `<section id="chat-workspace" data-messages-url="/projects/p1/messages" data-ws-url="/ws/projects/p1">
      <div id="chat-thread"></div>
      <form id="chat-composer"><textarea id="composer-content"></textarea><input id="composer-files" type="file" /></form>
    </section>`;
    initChatWorkspaces(host);
    const socket = FakeSocket.instances[0]!;
    socket.onopen?.();
    host.replaceChildren();

    socket.emit({ type: 'message.created', message_id: 'm1' });

    expect(socket.closed).to.equal(true);
  });
});
