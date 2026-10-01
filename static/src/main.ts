import htmx from 'htmx.org';
import { initChatWorkspaces } from './chat';
import { initShotProgress } from './shot';
import { CSRF_HEADER, csrfToken } from './components/lib/api';
import { lockDownHtmx } from './components/lib/htmx-config';
import { swapsErrorResponse } from './components/lib/thread';
import { requestFailureMessage, type RequestFailure } from './components/lib/errors';
import { showToast } from './ajax';
import { installHistorySnapshotHygiene } from './components/lib/history';
import { dedupeToastsOnSwap, dismissToastsOnClick } from './components/lib/toast';

// Attach the CSRF token (from <meta name="csrf-token">) to every HTMX request.
document.addEventListener('htmx:configRequest', (event) => {
  const { headers } = (event as CustomEvent<{ headers: Record<string, string> }>).detail;
  headers[CSRF_HEADER] = csrfToken();
});

// Error responses of actions are HTML: a toast (out-of-band, target untouched) or a
// re-rendered form with its errors (422). htmx does not swap 4xx/5xx by default.
document.addEventListener('htmx:beforeSwap', (event) => {
  const detail = (event as CustomEvent<{ xhr: XMLHttpRequest; shouldSwap: boolean; isError: boolean }>).detail;
  if (swapsErrorResponse(detail.xhr.status, detail.xhr.getResponseHeader('Content-Type'))) {
    detail.shouldSwap = true;
    detail.isError = false;
  }
});

// A request that got no swappable response (server down, timeout, a non-HTML error) would leave the
// screen unchanged: say so.
for (const type of ['htmx:responseError', 'htmx:sendError', 'htmx:timeout'] as const satisfies readonly RequestFailure[]) {
  document.addEventListener(type, (event) => {
    const xhr = (event as CustomEvent<{ xhr?: XMLHttpRequest }>).detail?.xhr;
    showToast(requestFailureMessage(type, xhr?.status));
  });
}

lockDownHtmx(htmx.config);

// Error toasts stay until their close button is pressed.
dismissToastsOnClick();
dedupeToastsOnSwap();

// A history snapshot must not keep init markers or the connection notice (issue 112).
installHistorySnapshotHygiene();

// The Chat Workspace can arrive as a full page or as an HTMX fragment.
document.addEventListener('DOMContentLoaded', () => {
  initChatWorkspaces();
  initShotProgress();
});
document.addEventListener('htmx:load', (event) => {
  initChatWorkspaces(event.target as ParentNode);
  initShotProgress(event.target as ParentNode);
});

// Lit components.
import './components/video-player/mam-video-player';
import './components/take-comparator/mam-take-comparator';
import './components/timeline-editor/mam-timeline-editor';
