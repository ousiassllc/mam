import { aTimeout, expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// htmx fires htmx:load on the top-level node of an outerHTML swap; that node is the Shot card (or the
// Chat Workspace) itself, so main.ts's initialisers have to treat the node as well as its descendants.
describe('outerHTML swap of a top-level node', () => {
  let host: HTMLElement;

  beforeEach(() => {
    host = document.createElement('div');
    document.body.append(host);
  });

  afterEach(() => host.remove());

  function swapOuter(targetId: string, html: string): void {
    htmx.swap(`#${targetId}`, html, { swapStyle: 'outerHTML', swapDelay: 0, settleDelay: 0 });
  }

  it('initialises a swapped Shot card, which then refreshes when its player finishes', async () => {
    host.innerHTML = '<div id="scene-shot-view"></div><ul><li id="shot-top">old</li></ul>';
    const requested: string[] = [];
    const onRequest = (event: Event) => {
      const path = (event as CustomEvent<{ pathInfo: { requestPath: string } }>).detail.pathInfo.requestPath;
      if (path.startsWith('/refresh/top')) {
        requested.push(path);
        event.preventDefault();
      }
    };
    document.body.addEventListener('htmx:beforeRequest', onRequest);
    try {
      swapOuter('shot-top', '<li id="shot-top" data-shot-progress data-refresh-url="/refresh/top">生成中</li>');
      await waitUntil(
        () => document.getElementById('shot-top')?.dataset.shotReady === 'true',
        'the swapped card was not initialised',
        { timeout: 2000 },
      );

      document.getElementById('shot-top')!.dispatchEvent(new Event('progress-finished'));
      await aTimeout(50);
      expect(requested).to.deep.equal(['/refresh/top']);
    } finally {
      document.body.removeEventListener('htmx:beforeRequest', onRequest);
    }
  });

  it('initialises a swapped Chat Workspace', async () => {
    host.innerHTML = '<div id="chat-workspace">old</div>';
    swapOuter(
      'chat-workspace',
      `<section id="chat-workspace" data-messages-url="/projects/p1/messages" data-ws-url="/ws/projects/p1">
         <div id="chat-thread"></div>
         <form id="chat-composer"><textarea id="composer-content"></textarea><input id="composer-files" type="file" /></form>
       </section>`,
    );
    await waitUntil(
      () => document.getElementById('chat-workspace')?.dataset.chatReady === 'true',
      'the swapped workspace was not initialised',
      { timeout: 2000 },
    );
  });
});
