// The starting roles of the pictures of a video form (#1009), without the DOM.

/**
 * The roles the pictures of a video form show, in the order of the pictures. While a ticked model cannot run without a first frame (an I2V model) and no
 * picture whose role the user chose is the first frame, the first picture whose role the user did not choose is the first frame; every other picture the
 * user did not choose a role for is a reference. A role the user chose (`touched`) is never moved, and every role stays visible in its select: this is only
 * where a picture starts, not a rewrite of what was asked.
 */
export function startingRoles(
  pictures: readonly { touched: boolean; role: string }[],
  needsFirstFrame: boolean,
): string[] {
  const taken = pictures.some((p) => p.touched && p.role === 'first_frame');
  let given = false;
  return pictures.map((p) => {
    if (p.touched) return p.role;
    if (needsFirstFrame && !taken && !given) {
      given = true;
      return 'first_frame';
    }
    return 'reference';
  });
}
