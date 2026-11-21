import { BADGE_TONE } from '../lib/badge';

/** The status of a Take as the server names it; the comparator colours it (the label is the server's `status_label`). */
export type TakeStatus = 'candidate' | 'ai_pick' | 'selected' | 'rejected';

/** The colours of the Shot card's `TakeStatusBadge` (molecules/status.templ): candidate = warning, AI Pick = info, selected = success, rejected = neutral. */
export const STATUS_CLASS: Record<TakeStatus, string> = {
  candidate: BADGE_TONE.warning,
  ai_pick: BADGE_TONE.info,
  selected: BADGE_TONE.success,
  rejected: BADGE_TONE.neutral,
};
