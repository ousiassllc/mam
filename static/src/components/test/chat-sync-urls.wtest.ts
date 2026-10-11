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
    const socket = mount('<article id="msg-m1" data-message-id="m1" data-card-url="/server/card-m1" data-next-url="/server/after-m1"></article>');
    socket.emit({ type: 'card.updated', message_id: 'm1' });
    await waitUntil(() => requested.length > 0, 'no request was made', { timeout: 2000 });
    expect(requested).to.deep.equal(['/server/card-m1']);
  });
});
