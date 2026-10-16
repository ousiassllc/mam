/** The status of a Take as the server names it; the comparator colours it (the label is the server's `status_label`). */
export type TakeStatus = 'candidate' | 'ai_pick' | 'selected' | 'rejected';

export const STATUS_CLASS: Record<TakeStatus, string> = {
  candidate: 'border-neutral-600 text-neutral-300',
  ai_pick: 'border-sky-500 text-sky-300',
  selected: 'border-emerald-500 text-emerald-300',
  rejected: 'border-neutral-700 text-neutral-400 line-through',
};
