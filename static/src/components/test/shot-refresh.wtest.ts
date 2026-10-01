import { aTimeout, expect, waitUntil } from '@open-wc/testing';
import '../../main';
import { initShotProgress } from '../../shot';

// A Shot that is not the one being viewed finishes: only its own card may be refreshed (#246).
describe('refresh of a finished Shot card', () => {
  let host: HTMLElement;
  let requests: { path: string; target: string }[];
  const onRequest = (event: Event) => {
    const detail = (event as CustomEvent<{ requestConfig: { path: string }; target: HTMLElement }>).detail;
    if (detail.requestConfig.path.startsWith('/projects/p/scenes/s')) {
      requests.push({ path: detail.requestConfig.path, target: detail.target.id });
      event.preventDefault();
    }
  };

  beforeEach(() => {
    requests = [];
    document.body.addEventListener('htmx:beforeRequest', onRequest);
    host = document.createElement('div');
    host.innerHTML = `
      <div id="scene-shot-view"><ol>
        <li id="shot-a" aria-current="true"><a href="/projects/p/scenes/s?shot=a">Shot 1</a></li>
        <li id="shot-b" data-shot-progress data-refresh-url="/projects/p/scenes/s?shot=b"><a href="/projects/p/scenes/s?shot=b">Shot 2</a></li>
      </ol></div>`;
    document.body.append(host);
    initShotProgress(host);
  });

  afterEach(() => {
    document.body.removeEventListener('htmx:beforeRequest', onRequest);
    host.remove();
  });

  it('refreshes only that card, from the selected Shot page, when another Shot is selected', async () => {
    document.getElementById('shot-b')!.dispatchEvent(new Event('progress-finished'));
    await waitUntil(() => requests.length > 0, 'no refresh was requested', { timeout: 2000 });
    expect(requests).to.deep.equal([{ path: '/projects/p/scenes/s?shot=a', target: 'shot-b' }]);
  });

  it('refreshes the whole Shot view when the selected Shot finishes', async () => {
    const a = document.getElementById('shot-a')!;
    a.dataset.shotProgress = '';
    a.dataset.refreshUrl = '/projects/p/scenes/s?shot=a';
    initShotProgress(host);
    a.dispatchEvent(new Event('progress-finished'));
    await waitUntil(() => requests.length > 0, 'no refresh was requested', { timeout: 2000 });
    await aTimeout(20);
    expect(requests).to.deep.equal([{ path: '/projects/p/scenes/s?shot=a', target: 'scene-shot-view' }]);
  });
});
