import htmx from 'htmx.org';
import { initChatWorkspaces } from './chat';
import { initShotProgress } from './shot';
import { CSRF_HEADER, csrfToken } from './components/lib/api';
import { lockDownHtmx } from './components/lib/htmx-config';
import { swapsErrorResponse } from './components/lib/thread';
import { requestFailureMessage, type RequestFailure } from './components/lib/errors';
import { showAlert } from './ajax';
import { installHistorySnapshotHygiene } from './components/lib/history';
import { installSaveOnChangeRevert } from './components/save-on-change/save-on-change';
import { installFocusRestore } from './components/focus-restore/focus-restore';
import { installInlineDialog } from './components/inline-dialog/inline-dialog';
import { dedupeAlertsOnSwap, dismissAlertsOnClick } from './components/alert/alert';
import { timeToastsOnSwap } from './components/toast/toast';
import { insideModal, installModal } from './components/modal/modal';
import { installCopy } from './components/copy/copy';
import { installHelp } from './components/help/help';
import { installSidenav } from './components/sidenav/sidenav';
import { installConfirm } from './components/confirm/confirm';
import { installScrim } from './components/scrim/scrim';
import { installTheme } from './components/theme/theme';
import { installRipple } from './components/ripple/ripple';
import { installModelPick } from './components/model-pick/model-pick';
import { initGentestInputs } from './components/gentest/inputs';
import { initGentestMatch } from './components/gentest/match';
import { initGentestAdvanced } from './components/gentest/advanced';
import { initGentestGptImage } from './components/gentest-gpt/gpt-image';
import { initGentestRefine } from './components/gentest-tiers/refine';
import { initGentestEasy } from './components/gentest-tiers/easy';
import { initGentestExposed } from './components/gentest-tiers/exposed';
import { initGentestRecipe } from './components/gentest-tiers/recipe';
import { installKeepScroll } from './components/scroll/keep-scroll';
import { installPollGuard } from './components/poll/poll-guard';
import { installExportPick } from './components/gentest/export-pick';
import { installSlider } from './components/slider/slider';
import { installRestartRefresh } from './components/restart/refresh';

// Attach the CSRF token (from <meta name="csrf-token">) to every HTMX request.
document.addEventListener('htmx:configRequest', (event) => {
  const { headers } = (event as CustomEvent<{ headers: Record<string, string> }>).detail;
  headers[CSRF_HEADER] = csrfToken();
});

// Error responses of actions are HTML: an alert (out-of-band, target untouched) or a
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
for (const type of [
  'htmx:responseError',
  'htmx:sendError',
  'htmx:timeout',
] as const satisfies readonly RequestFailure[]) {
  document.addEventListener(type, (event) => {
    // The modal says so itself: the alert region lies behind the dialog, which is in the top layer.
    if (insideModal(event.target as Element | null)) return;
    const xhr = (event as CustomEvent<{ xhr?: XMLHttpRequest }>).detail?.xhr;
    showAlert(requestFailureMessage(type, xhr?.status));
  });
}

// A control that saves on change (MODE, Production Template, Quality, Confirmation Level) has already
// moved to the chosen value when the request goes out. When the save fails, whatever the response
// (a 4xx/5xx alert, server down, timeout), put it back to the stored value, so that the screen never
// shows a value that was not saved (issue 538).
installSaveOnChangeRevert();

// The "?" helps: a button beside a heading opens its explanation; Esc closes it. Before the inline dialog: both listen
// to Esc on the document and the one that runs first takes it (preventDefault), so the help, which floats above the page,
// is what Esc closes first when a question of the model manager is open as well.
installHelp();

// The fold button of the side menu: folds it to its icons or opens it, and keeps the choice in a cookie that the server reads.
installSidenav();
installTheme();
installRipple();

// A confirmation (an `hx-confirm`) is asked in a centred modal, and a click on the scrim of a dialog closes it (the UI guide, O1, O2).
installConfirm();
installScrim();

// The model manager's questions in the page (age confirmation, license): the focus goes in, Esc closes, the focus goes back.
// Before the focus restore: a focus that is back on a control is not touched by it.
installInlineDialog();

// A swap that the user started and that replaced the control they used (a LockToggle button, a Delete
// button) would leave the focus on <body>: give it back to the control that took its place (issues 789, 794).
installFocusRestore();

// The modal dialog of the settings screen: opens when the body it was asked for arrives, closes on Esc / its buttons /
// a saved form, and gives the focus back to the control that opened it.
installModal();

// The "コピー" button of a value that is shown once (an MCP token that was just issued).
installCopy();

// The prompt match form of the generation test: the chosen image, the fields of the way to go on, the slider's value.
initGentestMatch();
// The detailed settings of the generation test (seed, steps, cfg, sampler, scheduler): shown while a ticked model takes them.
initGentestAdvanced();
initGentestGptImage();
initGentestRefine();
initGentestEasy();
initGentestExposed();
initGentestRecipe();
// The filter panel of the lists of models and the model picker (#1011): the funnel opens the panel, a card of the picker fills its field.
installModelPick();

// A scrolling box that a swap draws again (the grid of a running generation, which polls) keeps where it was scrolled to (#940).
installKeepScroll();

// A poll waits while the tab is hidden or a video in the section plays, so that the swap does not cut it off (#925).
installPollGuard();

// The results of the history that are tapped to be saved as a ZIP (#932): the glowing frame, the count, and the choice that survives the
// history's own refresh.
installExportPick();

// The sliders (every input[type=range]): the rail's fill follows the value (static/src/css/slider.css, #955).
installSlider();

// The "再起動して反映" panel of the settings screen is read again after a save (#1006, #1007).
installRestartRefresh();

lockDownHtmx(htmx.config);

// Error alerts stay until their close button is pressed; a toast goes by itself after 3 seconds.
dismissAlertsOnClick();
dedupeAlertsOnSwap();
timeToastsOnSwap();

// A history snapshot must not keep init markers or the connection notice (issue 112).
installHistorySnapshotHygiene();

// The Chat Workspace can arrive as a full page or as an HTMX fragment.
document.addEventListener('DOMContentLoaded', () => {
  initChatWorkspaces();
  initShotProgress();
  initGentestInputs();
});
document.addEventListener('htmx:load', (event) => {
  initChatWorkspaces(event.target as ParentNode);
  initShotProgress(event.target as ParentNode);
  initGentestInputs(event.target as ParentNode);
});

// Lit components.
import './components/video-player/mam-video-player';
import './components/take-comparator/mam-take-comparator';
import './components/timeline-editor/mam-timeline-editor';
import './components/comfy-frame/mam-comfy-frame';
import './components/mask-paint/mam-mask-paint';
import './components/prompt-weight/mam-prompt-weight';
import './components/prompt-weight/mam-weight-help';
import './components/prompt-weight/mam-weight-note';
