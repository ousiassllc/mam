import { expect } from '@open-wc/testing';

/** A short name for an element: chai prints a whole Lit component tree for a failed `equal` of two elements and hangs. */
const named = (el: Element | null | undefined): string => (el ? `${el.outerHTML.slice(0, 120)} @${path(el)}` : 'none');

function path(el: Element): string {
  const parts: string[] = [];
  for (let node: Element | null = el; node && node !== document.body; node = node.parentElement) {
    parts.unshift(`${node.tagName.toLowerCase()}${node.id ? `#${node.id}` : ''}`);
  }
  return parts.join('>');
}

/** Asserts which element holds the focus, with a readable message when it is another one. */
export function expectFocusOn(expected: Element | null | undefined): void {
  expect(named(document.activeElement)).to.equal(named(expected));
  expect(document.activeElement === expected, 'the same element, not a copy of it').to.equal(true);
}
