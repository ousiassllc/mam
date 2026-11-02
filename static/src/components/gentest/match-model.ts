// The rules of the prompt match form (#916) without the DOM: what a chosen image must be, which fields a way to go on shows, and how
// the target is written. match.ts draws them.

import { IMAGE_TYPES } from './inputs-model';

/** The way to go on that repeats until the target is reached: the only one that has a target and a limit of rounds. */
export const AUTO = 'auto';

/** Why a chosen file cannot be the reference image ("" when it can): the type, the size and an empty file. */
export function imageProblem(file: { type: string; size: number }, maxBytes: number): string {
  if (!IMAGE_TYPES.includes(file.type)) return '画像は PNG・JPEG・WebP にしてください';
  if (file.size === 0) return '空の画像ファイルは使えません';
  if (file.size > maxBytes) return `画像は ${Math.floor(maxBytes / (1 << 20))} MiB 以下にしてください`;
  return '';
}

/** Whether the target and the limit of rounds are shown for a way to go on. */
export const showsLimits = (mode: string): boolean => mode === AUTO;

/** The target as the field beside the slider writes it. */
export const targetText = (value: string | number): string => `${value}%`;
