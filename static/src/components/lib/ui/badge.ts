/** The shape of `atoms.Badge` (W6): a pill of 12px text, with the 1px edge of its tone. */
export const BADGE = 'inline-block rounded-full px-2 py-1 text-xs';

/**
 * The colours of the tones of `atoms.Badge` (atoms/badge.templ `badgeColors`): the state colour as the text, a 40% edge and a 10% ground
 * (brand guide C4). The neutral one keeps a transparent edge, so that it is as big as the others. One place for every Lit pill.
 */
export const BADGE_TONE = {
  info: 'border border-info/40 bg-info/10 text-info',
  warning: 'border border-warning/40 bg-warning/10 text-warning',
  success: 'border border-success/40 bg-success/10 text-success',
  neutral: 'border border-transparent bg-accent-soft text-ink',
} as const;
