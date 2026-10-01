/** The notice shown when a WebSocket has given up reconnecting: what stopped updating and how to get it back. */

import { createLogger } from './logger';
import { CONNECTION_LOST_MESSAGE } from './ws';

const REGION_ID = 'connection-lost-region';
const TEMPLATE_ID = 'connection-notice-template';
const log = createLogger('connection-notice');

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

/**
 * `what` is what stops updating ("チャット", "生成の進捗"). One notice per connection. The markup
 * is cloned from #connection-notice-template (atoms/connection_notice.templ); nothing is shown
 * when the page has no such template.
 */
export function createConnectionNotice(what: string): ConnectionNotice {
  let notice: HTMLElement | null = null;
  return {
    show(retry) {
      if (notice?.isConnected) return;
      const template = document.getElementById(TEMPLATE_ID);
      const clone = template instanceof HTMLTemplateElement ? template.content.firstElementChild?.cloneNode(true) : null;
      if (!(clone instanceof HTMLElement)) {
        log.warn(`#${TEMPLATE_ID} is missing; cannot show the connection notice`, what);
        return;
      }
      notice = clone;
      notice.dataset.connectionLost = what;
      const text = notice.querySelector('[data-connection-lost-message]');
      if (text) text.textContent = `${CONNECTION_LOST_MESSAGE}${what}が自動で更新されなくなりました。`;
      notice.querySelector('[data-action="reconnect"]')?.addEventListener('click', retry);
      notice.querySelector('[data-action="reload"]')?.addEventListener('click', () => location.reload());
      region().append(notice);
    },
    hide() {
      notice?.remove();
      notice = null;
    },
  };
}
