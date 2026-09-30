/**
 * Progress of a Shot that is being generated (F-12). The card is rendered by the server; this
 * module listens to /ws/shots/:id and, when the work is over, asks for the Shot's page fragment
 * again (the socket carries no HTML).
 */
import { ajaxGet } from './ajax';
import { connect, socketUrl, type Connection } from './components/lib/ws';
import { createRefreshGuard, isFinished, type ShotProgress } from './components/lib/shot';

const mayRefresh = createRefreshGuard();

export function initShotProgress(root: ParentNode = document): void {
  for (const el of root.querySelectorAll<HTMLElement>('[data-shot-progress]:not([data-shot-ready])')) {
    el.dataset.shotReady = 'true';
    watch(el);
  }
}

function watch(el: HTMLElement): void {
  const refreshUrl = el.dataset.refreshUrl ?? '';
  let connection: Connection | null = null;
  connection = connect<ShotProgress>({
    url: socketUrl(el.dataset.wsUrl ?? '', location),
    onMessage: (progress) => {
      // The card was replaced (another swap): this socket is no longer needed.
      if (!el.isConnected) {
        connection?.close();
        return;
      }
      if (progress.stage === 'generating' && progress.takes_total) {
        el.setAttribute('aria-label', `${progress.takes_done ?? 0}/${progress.takes_total}`);
      }
      if (isFinished(progress)) {
        connection?.close();
        if (refreshUrl && mayRefresh(refreshUrl)) void ajaxGet(refreshUrl, { target: '#scene-shot-view', swap: 'outerHTML' });
      }
    },
  });
}
