/**
 * The shape of `atoms.SmallButton` (atoms/button.templ `buttonClass`, O7): SemiBold 14px text, `rounded-md`, 32px high, the ripple of the press
 * (`data-ripple` in the markup) and a disabled button at opacity .4 (also `aria-disabled`, for a button that stays focusable). One place for every
 * Lit button, so that a change to the buttons of the guide is not missed in a copy of the class.
 */
export const BUTTON =
  'inline-flex min-h-8 items-center justify-center rounded-md px-3 py-1 text-sm font-semibold transition-colors ' +
  'disabled:cursor-not-allowed disabled:opacity-40 aria-disabled:cursor-not-allowed aria-disabled:opacity-40 ' +
  'focus-visible:focus-ring motion-reduce:transition-none';

/**
 * The look of `atoms.LinkButton` (the sub action: a --mute underline, Regular): the text-only button of a row. The same string as `linkButtonClass` of
 * atoms/button.templ; a11y/shape_test.go holds the two together.
 */
export const LINK_BUTTON =
  'text-sm text-mute underline transition-colors hover:text-ink disabled:cursor-not-allowed disabled:opacity-40 ' +
  'focus-visible:focus-ring motion-reduce:transition-none';

/** The tones of `atoms.SmallButton`: the main action (`TonePrimary`) and the sub one (`ToneNeutral`: a transparent face and the 1px --border). */
export const BUTTON_TONE = {
  primary: 'bg-accent text-bone hover:bg-accent/85',
  neutral: 'border border-border text-ink hover:bg-accent-soft',
} as const;
