// The rules of the input images of the generation test (spec/components/gentest-screen.md), without the DOM: which roles the
// chosen models take, which files are taken, and where a dragged image is dropped. inputs.ts draws them.

/** The roles of an image reference, in the order the select lists them. */
export const ROLE_ORDER = ['reference', 'face', 'style', 'edit'] as const;

/** The types of file the screen takes (the same as the service). */
export const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/webp'];

/** The value of the field `ref` of the n-th file (1-based) of the field `upload`; the server puts the staged file in its place. */
export const uploadRef = (n: number): string => `upload::#${n}`;

/** Whether a value of the field `ref` is an image chosen in the browser. */
export const isUploadRef = (ref: string): boolean => ref.startsWith('upload::#');

/** The roles that at least one of the chosen models takes, in the order of the select. */
export function unionRoles(lists: readonly (readonly string[])[]): string[] {
  const all = new Set(lists.flat());
  return ROLE_ORDER.filter((r) => all.has(r));
}

/** `items` with the one at `from` moved to index `to` (both clamped); the same array when nothing moves. */
export function moveItem<T>(items: readonly T[], from: number, to: number): T[] {
  const out = [...items];
  const target = Math.min(Math.max(to, 0), out.length - 1);
  if (from < 0 || from >= out.length || from === target) return out;
  const [item] = out.splice(from, 1);
  out.splice(target, 0, item as T);
  return out;
}

/** Whether a dragged image lands before or after the thumbnail under the pointer: the left half of a thumbnail is before it. */
export function dropSide(rect: { left: number; width: number }, x: number): 'before' | 'after' {
  return x < rect.left + rect.width / 2 ? 'before' : 'after';
}

/** The notice that the chosen models which take no input image will not use the ones that are chosen ("" for none to say). */
export function noRefNotice(refCount: number, models: readonly { label: string; accepts: boolean }[]): string {
  if (refCount === 0) return '';
  const names = models.filter((m) => !m.accepts).map((m) => m.label);
  return names.length > 0 ? `${names.join('、')} は入力画像を使いません` : '';
}

/** What `takeFiles` decided. */
export interface Taken {
  files: File[];
  /** The one short line to show ("" when every file was taken). */
  error: string;
}

/**
 * The files to add: the images of an allowed type that are not too large and that fit in `room`. What was left out is said in one
 * line (the rules are not on the page until one is broken).
 */
export function takeFiles(files: readonly File[], room: number, maxBytes: number, max: number): Taken {
  const images = files.filter((f) => IMAGE_TYPES.includes(f.type));
  const sized = maxBytes > 0 ? images.filter((f) => f.size <= maxBytes) : images;
  const taken = sized.slice(0, Math.max(room, 0));
  let error = '';
  if (taken.length < sized.length) error = `最大 ${max} 枚です`;
  else if (sized.length < images.length)
    error = `大きすぎる画像は除きました（1 枚 ${Math.floor(maxBytes / (1 << 20))} MiB まで）`;
  else if (images.length < files.length) error = 'PNG・JPEG・WebP の画像だけ選べます';
  return { files: taken, error };
}
