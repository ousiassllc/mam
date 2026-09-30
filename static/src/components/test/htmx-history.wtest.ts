import { aTimeout, expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';
import { initShotProgress } from '../../shot';
import { sanitizeHistoryHtml } from '../lib/history';

const OTHER = '/src/components/testdata/history-shot-b.html';

describe('htmx history snapshot', () => {
  const startHref = location.href;
  let leftPage = false;

  afterEach(async () => {
    document.getElementById('history-root')?.remove();
    sessionStorage.removeItem('htmx-history-cache');
    if (leftPage) {
      leftPage = false;
      history.back();
      await waitUntil(() => location.href === startHref, 'did not return to the test page', { timeout: 2000 });
    }
  });

  it('drops ready markers and the connection notice', () => {
    const saved = sanitizeHistoryHtml(`
      <article data-shot-progress data-shot-ready="true">生成中</article>
      <div id="chat-workspace" data-chat-ready="true"></div>
      <div id="connection-lost-region"><button type="button">再接続</button></div>
    `);
    expect(saved).to.contain('data-shot-progress');
    expect(saved).to.contain('chat-workspace');
    expect(saved).to.not.contain('data-shot-ready');
    expect(saved).to.not.contain('data-chat-ready');
    expect(saved).to.not.contain('connection-lost-region');
  });

  it('drops the light-DOM paint of mam-* elements and keeps their attributes', () => {
    const saved = sanitizeHistoryHtml(`
      <mam-video-player video-src="/a.webm"><video></video><p>hello</p></mam-video-player>
      <mam-take-comparator api-url="/takes"><div>old</div></mam-take-comparator>
      <mam-timeline-editor api-url="/edit"><span>old</span></mam-timeline-editor>
    `);
    expect(saved).to.contain('video-src="/a.webm"');
    expect(saved).to.contain('api-url="/takes"');
    expect(saved).to.contain('api-url="/edit"');
    expect(saved).to.not.contain('<video');
    expect(saved).to.not.contain('hello');
    expect(saved).to.not.contain('old');
  });

  it('watches a generating card again after Back restores the scene', async () => {
    sessionStorage.removeItem('htmx-history-cache');
    const root = document.createElement('div');
    root.id = 'history-root';
    root.setAttribute('hx-history-elt', '');
    root.innerHTML = `
      <main id="scene-page">
        <div id="scene-shot-view"></div>
        <article id="card" data-shot-progress data-refresh-url="/refresh/back">生成中</article>
        <div id="connection-lost-region"><button type="button">再接続</button></div>
        <p data-chat-ready="true" id="chat-marker">chat</p>
        <button id="open-shot" type="button" hx-get="${OTHER}" hx-target="#history-root" hx-swap="innerHTML" hx-push-url="true">開く</button>
      </main>`;
    document.body.append(root);
    htmx.process(root);
    initShotProgress(root);
    expect(document.getElementById('card')!.dataset.shotReady).to.equal('true');

    const refreshUrls: string[] = [];
    const onRequest = (event: Event) => {
      const path = (event as CustomEvent<{ pathInfo: { requestPath: string } }>).detail.pathInfo.requestPath;
      if (path.startsWith('/refresh/')) {
        refreshUrls.push(path);
        event.preventDefault();
      }
    };
    document.body.addEventListener('htmx:beforeRequest', onRequest);

    try {
      (document.getElementById('open-shot') as HTMLButtonElement).click();
      leftPage = true;
      await waitUntil(() => document.getElementById('other-shot') !== null, 'the other shot did not open', { timeout: 3000 });

      const cached = sessionStorage.getItem('htmx-history-cache') ?? '';
      expect(cached).to.contain('data-shot-progress');
      expect(cached).to.not.contain('data-shot-ready');
      expect(cached).to.not.contain('data-chat-ready');
      expect(cached).to.not.contain('connection-lost-region');

      history.back();
      await waitUntil(() => document.getElementById('card') !== null, 'the scene was not restored', { timeout: 3000 });
      leftPage = false;

      const card = document.getElementById('card')!;
      card.dispatchEvent(new Event('progress-finished'));
      await aTimeout(50);
      expect(refreshUrls).to.deep.equal(['/refresh/back']);
    } finally {
      document.body.removeEventListener('htmx:beforeRequest', onRequest);
    }
  });
});
