/**
 * Progress of a Shot that is being generated (F-12). The card is rendered by the server; when the
 * work is over this module asks for the Shot's page fragment again (the socket carries no HTML).
 * A card has one /ws/shots/:id connection: its mam-video-player holds it when there is one (this
 * module waits for the player's progress-finished), otherwise data-ws-url names the socket and this
 * module listens to it.
 */
import { ajaxGet } from './ajax';
import { createConnectionNotice } from './components/lib/connection-notice';
import { connect, socketUrl, type Connection } from './components/lib/ws';
import { selfAndDescendants } from './components/lib/query';
import { createRefreshGuard, isFinished, planCardRefresh, type ShotProgress } from './components/lib/shot';

const mayRefresh = createRefreshGuard();

export function initShotProgress(root: ParentNode = document): void {
  for (const el of selfAndDescendants<HTMLElement>(root, '[data-shot-progress]:not([data-shot-ready])')) {
    el.dataset.shotReady = 'true';
    watch(el);
  }
}

function refresh(card: HTMLElement, stage: ShotProgress['stage']): void {
  const refreshUrl = card.dataset.refreshUrl ?? '';
  if (!refreshUrl || !mayRefresh(refreshUrl, stage)) return;
  const selectedLink = document.querySelector('#scene-shot-view li[id^="shot-"][aria-current="true"] a[href]');
  const plan = planCardRefresh(
    { id: card.id, refreshUrl, selected: card.getAttribute('aria-current') === 'true' },
    selectedLink?.getAttribute('href') ?? null,
  );
  void ajaxGet(plan.url, { target: plan.target, swap: plan.swap, select: plan.select });
}

function watch(el: HTMLElement): void {
  if (!el.dataset.wsUrl) {
    // The card's mam-video-player follows the progress and says when it is over.
    el.addEventListener('progress-finished', (event) => refresh(el, (event as CustomEvent<ShotProgress | undefined>).detail?.stage ?? 'done'), { once: true });
    return;
  }
  let connection: Connection | null = null;
  const notice = createConnectionNotice('生成の進捗');
  connection = connect<ShotProgress>({
    url: socketUrl(el.dataset.wsUrl ?? '', location),
    onOpen: () => notice.hide(),
    onGiveUp: () => {
      // The card was replaced meanwhile: nobody is waiting for this socket any more.
      if (!el.isConnected) return;
      notice.show(() => {
        if (el.isConnected) connection?.retry();
        else {
          notice.hide();
          connection?.close();
        }
      });
    },
    onMessage: (progress) => {
      // The card was replaced (another swap): this socket is no longer needed.
      if (!el.isConnected) {
        notice.hide();
        connection?.close();
        return;
      }
      if (progress.stage === 'generating' && progress.takes_total) {
        el.setAttribute('aria-label', `${progress.takes_done ?? 0}/${progress.takes_total}`);
      }
      if (isFinished(progress)) {
        notice.hide();
        connection?.close();
        refresh(el, progress.stage);
      }
    },
  });
}
