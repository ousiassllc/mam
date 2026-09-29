import htmx from 'htmx.org';
import { CSRF_HEADER, csrfToken } from './components/lib/api';

// Attach the CSRF token (from <meta name="csrf-token">) to every HTMX request.
document.addEventListener('htmx:configRequest', (event) => {
  const { headers } = (event as CustomEvent<{ headers: Record<string, string> }>).detail;
  headers[CSRF_HEADER] = csrfToken();
});

// The front end is served by the app itself: never let htmx call external hosts.
htmx.config.selfRequestsOnly = true;

// Lit components (mam-video-player, mam-timeline-editor, mam-take-comparator,
// mam-branch-tree) are imported here as they are implemented.
