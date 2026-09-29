import htmx from 'htmx.org';
import { initChatWorkspaces } from './chat';
import { CSRF_HEADER, csrfToken } from './components/lib/api';
import { swapsErrorResponse } from './components/lib/thread';

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

// The front end is served by the app itself: never let htmx call external hosts.
htmx.config.selfRequestsOnly = true;

// Toasts disappear by themselves.
new MutationObserver((records) => {
  for (const record of records) {
    for (const node of record.addedNodes) {
      if (node instanceof HTMLElement && node.hasAttribute('data-toast')) {
        setTimeout(() => node.remove(), 6000);
      }
    }
  }
}).observe(document.documentElement, { childList: true, subtree: true });

// The Chat Workspace can arrive as a full page or as an HTMX fragment.
document.addEventListener('DOMContentLoaded', () => initChatWorkspaces());
document.addEventListener('htmx:load', (event) => initChatWorkspaces(event.target as ParentNode));

// Lit components (mam-video-player, mam-timeline-editor, mam-take-comparator,
// mam-branch-tree) are imported here as they are implemented.
