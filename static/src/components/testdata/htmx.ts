import { waitUntil } from '@open-wc/testing';

/**
 * Waits until htmx has settled every swap that was drawn (no element is `htmx-settling` any more). htmx processes the new
 * content (the hx-* attributes of a control that arrived) and gives the focus back only when a swap settles, which is a
 * timer (the settle delay) after the swap: a test that clicks or presses a key on what a swap just brought, before this, acts
 * on a control that htmx does not know yet, and under load nothing happens. Wait for the visible result of the swap first
 * (the swap adds the class), then for this.
 */
export async function swapSettled(): Promise<void> {
  await waitUntil(() => document.querySelector('.htmx-settling') === null, 'the swap did not settle', {
    timeout: 2000,
  });
}
