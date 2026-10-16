/** Where the focus goes (spec/components/overview.md §5.2) when the control of a Take row or of the selection panel was pressed. */
type Targets = (Element | null)[];

/** The same button of the Take's row, the other one, the row's heading, the component. */
export function rowTargets(host: HTMLElement, takeId: string, action: 'select' | 'reject'): Targets {
  const row = host.querySelector(`[data-take="${CSS.escape(takeId)}"]`);
  const other = action === 'select' ? 'reject' : 'select';
  return [
    row?.querySelector(`[data-action="${action}"]`) ?? null,
    row?.querySelector(`[data-action="${other}"]`) ?? null,
    row?.querySelector('th[scope="row"]') ?? null,
    host,
  ];
}

/** The first control of the selection panel that `selectors` find, then the panel itself (`tabindex="-1"`). */
export function selectionTargets(host: HTMLElement, ...selectors: string[]): Targets {
  const panel = host.querySelector('[data-selection]');
  return [...selectors.map((s) => panel?.querySelector(s) ?? null), panel];
}

/** The "×" of the part now at `index` (the last one when the removed part was the last). */
export function segmentButton(index: number, count: number): string {
  return `[data-segment]:nth-child(${Math.min(index, count - 1) + 1}) button`;
}
