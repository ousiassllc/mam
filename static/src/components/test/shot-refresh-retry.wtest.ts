import { expect, waitUntil } from '@open-wc/testing';
import { initShotProgress } from '../../shot';
import { FakeSocket, installConnectionNoticeTemplate, stubWebSocket } from '../testdata/fakes';

// The "work is over" notice comes once and the socket is closed: a refresh that fails must be retried by the
// card itself, or it stays "generating" until the page is reloaded.
describe('Shot card: refresh after the work is over (shot.ts)', () => {
  let restoreSocket: () => void;
  let removeTemplate: () => void;
  beforeEach(() => {
    restoreSocket = stubWebSocket();
    removeTemplate = installConnectionNoticeTemplate();
  });
  afterEach(() => {
    removeTemplate();
    restoreSocket();
  });

  it('retries a refresh that failed, and replaces the card once it works', async function () {
    this.timeout(8000); // the retry waits for its first backoff (1 s)
    // Built by hand: fixture() waits for an animation frame, which a background tab (parallel runs) never gives.
    const root = document.createElement('div');
    root.innerHTML =
      '<div id="scene-shot-view" aria-current="true" data-shot-progress data-refresh-url="/server/missing-shot" data-ws-url="/ws/shots/s9"></div>';
    document.body.append(root);
    const failed: number[] = [];
    const onAfterRequest = (event: Event) => {
      const detail = (event as CustomEvent<{ xhr: { status: number }; pathInfo: { requestPath: string } }>).detail;
      if (detail.pathInfo.requestPath === '/server/missing-shot') failed.push(detail.xhr.status);
    };
    document.body.addEventListener('htmx:afterRequest', onAfterRequest);
    try {
      initShotProgress(root);
      FakeSocket.instances[0]!.emit({ shot_id: 's9', stage: 'done' });
      await waitUntil(() => failed.length > 0, 'the first refresh did not fail', { timeout: 2000 });
      // The server is back: the card now points at a fragment that exists.
      root.querySelector<HTMLElement>('#scene-shot-view')!.dataset.refreshUrl =
        '/src/components/testdata/shot-view-final.html';
      await waitUntil(
        () => root.querySelector('#scene-shot-view[data-refreshed]') !== null,
        'the card was not replaced',
        {
          timeout: 5000,
        },
      );
      expect(failed).to.deep.equal([404]);
    } finally {
      document.body.removeEventListener('htmx:afterRequest', onAfterRequest);
      root.remove();
    }
  });
});
