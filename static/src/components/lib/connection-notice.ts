/** The notice shown when a WebSocket has given up reconnecting: what stopped updating and how to get it back. */

import { CONNECTION_LOST_MESSAGE } from './ws';

const REGION_ID = 'connection-lost-region';

export interface ConnectionNotice {
  /** Shows the notice; "再接続" calls `retry`. */
  show(retry: () => void): void;
  /** Removes it (the connection is back). */
  hide(): void;
}

function region(): HTMLElement {
  let el = document.getElementById(REGION_ID);
  if (!el) {
    el = document.createElement('div');
    el.id = REGION_ID;
    el.className = 'pointer-events-none fixed bottom-4 left-4 z-50 flex flex-col gap-2';
    document.body.append(el);
  }
  return el;
}

/** `what` is what stops updating ("チャット", "生成の進捗"). One notice per connection. */
export function createConnectionNotice(what: string): ConnectionNotice {
  let notice: HTMLElement | null = null;
  return {
    show(retry) {
      if (notice?.isConnected) return;
      notice = document.createElement('div');
      notice.setAttribute('role', 'alert');
      notice.dataset.connectionLost = what;
      notice.className = 'pointer-events-auto flex items-center gap-3 rounded-md border border-amber-500/50 bg-neutral-900 px-4 py-2 text-sm text-amber-200 shadow-lg';
      const text = document.createElement('span');
      text.textContent = `${CONNECTION_LOST_MESSAGE}${what}が自動で更新されなくなりました。`;
      const reconnect = document.createElement('button');
      reconnect.type = 'button';
      reconnect.textContent = '再接続';
      reconnect.className = 'rounded border border-amber-500/50 px-2 py-1 hover:bg-neutral-800';
      reconnect.addEventListener('click', retry);
      const reload = document.createElement('button');
      reload.type = 'button';
      reload.textContent = '再読み込み';
      reload.className = reconnect.className;
      reload.addEventListener('click', () => location.reload());
      notice.append(text, reconnect, reload);
      region().append(notice);
    },
    hide() {
      notice?.remove();
      notice = null;
    },
  };
}
