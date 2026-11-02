/**
 * Keeps where a scrolling box was scrolled to when an HTMX swap draws it again (#940).
 *
 * A section that asks for itself every 2 seconds (the grid of a running generation) is replaced as a whole, so the box that
 * scrolls inside it is a new element, at its left end. A box marked `data-keep-scroll="<key>"` is put back where the reader had
 * left it, from the box with the same key that the swap took away. A box that was at its right (bottom) end follows that end
 * when the content grew; one that was anywhere else is never moved to an end, so that a reader looking at the middle is not
 * thrown by what arrives.
 *
 * The positions are read when a response is about to be swapped in (`htmx:beforeSwap`: the old boxes are still in the page) and
 * put back after the swap (`htmx:afterSwap`: the new ones are in, the page is not drawn yet, so nothing flashes). A box that the
 * swap left alone (the same element) is not touched.
 */

const SELECTOR = '[data-keep-scroll]';

/** Where a box was scrolled to. `atRight` / `atBottom`: it was at that end of content that did not fit. */
interface Place {
  box: Element;
  left: number;
  top: number;
  atRight: boolean;
  atBottom: boolean;
}

/** The slack of a fractional scroll position (zoom, high-density screens). */
const END_SLACK = 1;

function placeOf(box: Element): Place {
  return {
    box,
    left: box.scrollLeft,
    top: box.scrollTop,
    atRight: box.scrollWidth > box.clientWidth && box.scrollLeft + box.clientWidth >= box.scrollWidth - END_SLACK,
    atBottom: box.scrollHeight > box.clientHeight && box.scrollTop + box.clientHeight >= box.scrollHeight - END_SLACK,
  };
}

function keyOf(box: Element): string {
  return box.getAttribute('data-keep-scroll') ?? '';
}

/** Puts a new box where `place` was: the end it was at, or the position it had (the browser keeps it inside the content). */
function putBack(box: Element, place: Place): void {
  box.scrollLeft = place.atRight ? box.scrollWidth : place.left;
  box.scrollTop = place.atBottom ? box.scrollHeight : place.top;
}

export function installKeepScroll(root: Document | HTMLElement = document): void {
  let places = new Map<string, Place>();

  root.addEventListener('htmx:beforeSwap', () => {
    places = new Map();
    for (const box of document.querySelectorAll(SELECTOR)) places.set(keyOf(box), placeOf(box));
  });

  root.addEventListener('htmx:afterSwap', () => {
    for (const box of document.querySelectorAll(SELECTOR)) {
      const place = places.get(keyOf(box));
      if (place && place.box !== box) putBack(box, place);
    }
    places = new Map();
  });
}
