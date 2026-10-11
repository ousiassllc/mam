/**
 * The elements matching `selector` in `root` and below it. htmx fires htmx:load on the top-level
 * node of an `outerHTML` swap, so a module that initialises elements on that event has to look at the
 * node itself too, which `querySelectorAll` alone never returns.
 */
export function selfAndDescendants<T extends Element>(root: ParentNode, selector: string): T[] {
  const found = [...root.querySelectorAll<T>(selector)];
  if (root instanceof Element && root.matches(selector)) found.unshift(root as unknown as T);
  return found;
}
