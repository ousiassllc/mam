/** The status of a Take as the server names it; the comparator colours it (the label is the server's `status_label`). */
export type TakeStatus = 'candidate' | 'ai_pick' | 'selected' | 'rejected';

/** The shape of `atoms.Badge` (W6): a pill of 12px text. */
export const BADGE = 'inline-block rounded-full px-2 py-1 text-xs';

/** The colours of the Shot card's `TakeStatusBadge` (molecules/status.templ): candidate = warning, AI Pick = info, selected = success, rejected = neutral. */
export const STATUS_CLASS: Record<TakeStatus, string> = {
  candidate: 'bg-warning/20 text-warning',
  ai_pick: 'bg-info/20 text-info',
  selected: 'bg-success/20 text-success',
  rejected: 'bg-accent-soft text-ink',
};
