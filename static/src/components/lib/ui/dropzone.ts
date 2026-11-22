/**
 * The drop highlight of a drop zone (brand guide §7: the zone at rest has a dashed --border, while a file is dragged over it it is --accent-soft with a dashed
 * --accent). The classes are those of the colour table (spec/components/color-rules.md), swapped in by the script: the resting colour is taken off before the
 * active one is put on, because Tailwind writes `border-border` after `border-accent` and the later rule would win.
 */

const ZONE_REST = 'border-border';
const ZONE_ACTIVE = ['border-accent', 'bg-accent-soft'];

/** Swaps the colour classes of a drop target: `active` on while `on`, `rest` otherwise. */
export function highlight(el: Element, on: boolean, rest: string, active: string[]): void {
  el.classList.toggle(rest, !on);
  for (const c of active) el.classList.toggle(c, on);
}

/** Puts the highlight of a drop zone on (a file is over it) or off. */
export function highlightZone(zone: Element, on: boolean): void {
  highlight(zone, on, ZONE_REST, ZONE_ACTIVE);
}
