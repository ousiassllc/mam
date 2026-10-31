import { aTimeout, expect, fixtureSync, html, waitUntil } from '@open-wc/testing';
import './mam-video-player';
import type { MamVideoPlayer } from './mam-video-player';
import { FakeSocket, stubWebSocket } from '../testdata/fakes';
import { initShotProgress } from '../../shot';

// The refresh is an htmx GET into #scene-shot-view: the test only watches it start.
function refreshes(): { urls: string[]; stop: () => void } {
  const urls: string[] = [];
  const on = (e: Event) =>
    urls.push((e as CustomEvent<{ pathInfo: { requestPath: string } }>).detail.pathInfo.requestPath);
  document.body.addEventListener('htmx:beforeRequest', on);
  return { urls, stop: () => document.body.removeEventListener('htmx:beforeRequest', on) };
}

describe('Shot card: one WebSocket per card (shot.ts)', () => {
  let restoreSocket: () => void;
  beforeEach(() => {
    restoreSocket = stubWebSocket();
  });
  afterEach(() => restoreSocket());

  it('a card with a player is followed by the player alone, and is refreshed when the player says it is over', async () => {
    const watch = refreshes();
    // Not `fixture`: the wrapper is no Lit element, so it would wait for an animation frame, and a starved page
    // can go for seconds without presenting one.
    const root = fixtureSync(html`<div>
      <div id="scene-shot-view"></div>
      <ul>
        <li id="shot-player" aria-current="true" data-shot-progress data-refresh-url="/refresh/player">
          <mam-video-player ws-url="/ws/shots/s1"></mam-video-player>
        </li>
      </ul>
    </div>`);
    await root.querySelector<MamVideoPlayer>('mam-video-player')!.updateComplete;
    initShotProgress(root);

    expect(FakeSocket.instances.map((s) => s.url)).to.deep.equal([
      `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws/shots/s1`,
    ]);

    FakeSocket.instances[0]!.emit({ shot_id: 's1', stage: 'generating', takes_done: 1, takes_total: 3 });
    await aTimeout(20);
    expect(watch.urls).to.deep.equal([]);

    FakeSocket.instances[0]!.emit({ shot_id: 's1', stage: 'done' });
    await waitUntil(() => watch.urls.length > 0, 'the card was not refreshed', { timeout: 5000 });
    await aTimeout(50); // a second refresh would show up by now
    watch.stop();
    expect(watch.urls).to.deep.equal(['/refresh/player']);
    expect(FakeSocket.instances.length).to.equal(1);
  });

  it('a card without a player is followed over data-ws-url, and is refreshed when the work is over', async () => {
    const watch = refreshes();
    const root = fixtureSync(html`<div>
      <div id="scene-shot-view"></div>
      <ul>
        <li id="shot-socket" aria-current="true" data-shot-progress data-refresh-url="/refresh/socket" data-ws-url="/ws/shots/s2"></li>
      </ul>
    </div>`);
    initShotProgress(root);
    expect(FakeSocket.instances.length).to.equal(1);
    expect(FakeSocket.instances[0]!.url).to.contain('/ws/shots/s2');

    FakeSocket.instances[0]!.emit({ shot_id: 's2', stage: 'failed' });
    await waitUntil(() => watch.urls.length > 0, 'the card was not refreshed', { timeout: 5000 });
    await aTimeout(50); // a second refresh would show up by now
    watch.stop();
    expect(watch.urls).to.deep.equal(['/refresh/socket']);
  });
});
