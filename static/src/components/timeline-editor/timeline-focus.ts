/** Where the focus goes (spec/components/overview.md §5.2) when the control of a clip row or of the toolbar was pressed. */
import type { LitElement } from 'lit';
import { focusAfterUpdate } from '../focus-restore/lit-focus';

type Targets = (Element | null)[];

/**
 * The first of `actions` on the row of the clip `shotId` (a disabled one is skipped), then the row itself
 * (`tabindex="-1"`), then the list.
 */
function clipTargets(host: HTMLElement, shotId: string | undefined, ...actions: string[]): Targets {
  const row = shotId === undefined ? null : host.querySelector(`[data-clip="${CSS.escape(shotId)}"]`);
  return [
    ...actions.map((a) => row?.querySelector(`[data-action="${a}"]`) ?? null),
    row,
    host.querySelector('[data-clips]'),
  ];
}

/** After "↑" / "↓": the same button of the moved clip, or the other one when the clip reached the end of the list. */
export function focusMoved(host: LitElement, shotId: string, from: number, to: number): void {
  const [first, second] = to < from ? ['up', 'down'] : ['down', 'up'];
  void focusAfterUpdate(host, () => clipTargets(host, shotId, first, second));
}

/** After "外す": the "外す" of the clip that took the removed one's place (its other buttons when it is the last one left). */
export function focusRemoved(host: LitElement, shotId: string | undefined): void {
  void focusAfterUpdate(host, () => clipTargets(host, shotId, 'remove', 'up', 'down'));
}

/** After a save or a reload: the Save button when it is enabled again (the save failed), else the "保存しました" notice, else the list. */
export function focusToolbar(host: LitElement): void {
  void focusAfterUpdate(host, () => [
    host.querySelector('[data-action="save"]'),
    host.querySelector('[data-notice]'),
    host.querySelector('[data-clips]'),
    host,
  ]);
}
