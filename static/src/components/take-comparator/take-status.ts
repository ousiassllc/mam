/** The status of a Take as the server names it; the comparator colours it (the label is the server's `status_label`). */
export type TakeStatus = 'candidate' | 'ai_pick' | 'selected' | 'rejected';

export const STATUS_CLASS: Record<TakeStatus, string> = {
  candidate: 'border-border text-ink',
  ai_pick: 'border-info/60 text-info',
  selected: 'border-success/60 text-success',
  rejected: 'border-border text-mute line-through',
};
