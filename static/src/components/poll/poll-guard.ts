/**
 * Holds back a poll that would spoil what the reader is doing (#925).
 *
 * A section with a running job asks for itself again every 2 seconds (`hx-trigger="every 2s"`) and the answer replaces it, so
 * the media in it start over. Two cases are held back, and the next poll (htmx keeps its timer) tries again:
 * - the tab is hidden: nobody sees the answer; the first poll after the tab is shown brings the screen up to date;
 * - a <video> or <audio> in the section is playing: a swap would cut it off and start it again from the first frame.
 * A request the reader makes (the refresh button, a click) is never held back.
 */

/** Whether an `hx-trigger` has a poll: "every 2s", also in a list ("click, every 2s"). */
const POLL = /(^|,)\s*every\s/;

interface RequestDetail {
  elt: Element;
  requestConfig?: { triggeringEvent?: unknown };
}

function isPoll(detail: RequestDetail): boolean {
  // A request of the reader has the event that made it (a click, a submit); a poll has none.
  if (detail.requestConfig?.triggeringEvent instanceof Event) return false;
  return POLL.test(detail.elt.getAttribute('hx-trigger') ?? '');
}

function mediaPlaying(el: Element): boolean {
  return [...el.querySelectorAll<HTMLMediaElement>('video, audio')].some((m) => !m.paused && !m.ended);
}

export function installPollGuard(root: Document | HTMLElement = document): void {
  root.addEventListener('htmx:beforeRequest', (event) => {
    const detail = (event as CustomEvent<RequestDetail>).detail;
    if (!isPoll(detail)) return;
    if (document.hidden || mediaPlaying(detail.elt)) event.preventDefault();
  });
}
