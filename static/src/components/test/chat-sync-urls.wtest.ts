import { expect, waitUntil } from '@open-wc/testing';
import '../../main';
import { initChatWorkspaces } from '../../chat';
import { FakeSocket, installConnectionNoticeTemplate, stubWebSocket } from '../testdata/fakes';

// The chat asks for messages and cards at the URLs the server put on the page (data-messages-url,
// data-next-url, data-card-url); it builds none itself.
describe('URLs the Chat Workspace requests', () => {
  let restoreSocket: () => void;
  let removeTemplate: () => void;
  let host: HTMLElement;
  let requested: string[];
  const onRequest = (event: Event) => {
    const path = (event as CustomEvent<{ requestConfig: { path: string } }>).detail.requestConfig.path;
    if (path.startsWith('/server/')) {
      requested.push(path);
      event.preventDefault();
    }
  };

  beforeEach(() => {
    restoreSocket = stubWebSocket();
    removeTemplate = installConnectionNoticeTemplate();
    requested = [];
    document.body.addEventListener('htmx:beforeRequest', onRequest);
    host = document.createElement('div');
    document.body.append(host);
  });

  afterEach(() => {
    document.body.removeEventListener('htmx:beforeRequest', onRequest);
    host.remove();
    removeTemplate();
    restoreSocket();
  });

  function mount(thread: string): FakeSocket {
    host.innerHTML = `<section id="chat-workspace" data-messages-url="/server/all" data-ws-url="/ws/projects/p1">
      <div id="chat-thread">${thread}</div>
      <form id="chat-composer"><textarea id="composer-content"></textarea><input id="composer-files" type="file" /></form>
    </section>`;
    initChatWorkspaces(host);
    return FakeSocket.instances[0]!;
  }

  it('fetches the whole thread from data-messages-url when no message is shown', async () => {
    const socket = mount('');
    socket.emit({ type: 'message.created', message_id: 'm1' });
    await waitUntil(() => requested.length > 0, 'no request was made', { timeout: 2000 });
    expect(requested).to.deep.equal(['/server/all']);
  });

  it('fetches what is newer from data-next-url of the last message', async () => {
    const socket = mount(`
      <article id="msg-m1" data-message-id="m1" data-next-url="/server/after-m1"></article>
      <article id="msg-m2" data-message-id="m2" data-next-url="/server/after-m2"></article>`);
    socket.emit({ type: 'message.created', message_id: 'm3' });
    await waitUntil(() => requested.length > 0, 'no request was made', { timeout: 2000 });
    expect(requested).to.deep.equal(['/server/after-m2']);
  });

  it('refetches a card from its own data-card-url', async () => {
    const socket = mount(
      '<article id="msg-m1" data-message-id="m1" data-card-url="/server/card-m1" data-next-url="/server/after-m1"></article>',
    );
    socket.emit({ type: 'card.updated', message_id: 'm1' });
    await waitUntil(() => requested.length > 0, 'no request was made', { timeout: 2000 });
    expect(requested).to.deep.equal(['/server/card-m1']);
  });

  it('retries a card refetch that failed, and replaces the card once it works', async function () {
    this.timeout(8000); // the retry waits for its first backoff (1 s)
    const socket = mount(
      '<article id="msg-m1" data-message-id="m1" data-card-pending data-card-url="/server/missing-card"></article>',
    );
    const failed: number[] = [];
    const onAfterRequest = (event: Event) => {
      const detail = (event as CustomEvent<{ xhr: { status: number }; pathInfo: { requestPath: string } }>).detail;
      if (detail.pathInfo.requestPath === '/server/missing-card') failed.push(detail.xhr.status);
    };
    document.body.addEventListener('htmx:afterRequest', onAfterRequest);
    try {
      document.body.removeEventListener('htmx:beforeRequest', onRequest); // let the requests go to the dev server
      socket.emit({ type: 'card.updated', message_id: 'm1' });
      await waitUntil(() => failed.length > 0, 'the first refetch did not fail', { timeout: 2000 });
      // The server is back: the card now points at a fragment that exists.
      host.querySelector<HTMLElement>('#msg-m1')!.dataset.cardUrl = '/src/components/testdata/card-final.html';
      await waitUntil(() => host.querySelector('#msg-m1[data-card-final]') !== null, 'the card was not replaced', {
        timeout: 5000,
      });
      expect(failed).to.deep.equal([404]);
    } finally {
      document.body.removeEventListener('htmx:afterRequest', onAfterRequest);
    }
  });
});
