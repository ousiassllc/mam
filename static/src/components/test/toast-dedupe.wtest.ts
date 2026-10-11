import { expect } from '@open-wc/testing';
import '../../main';

// The Cost Summary polls every 15s whatever the outcome (hx-trigger="every 15s"): a server that is down
// raises htmx:sendError each time, which must not stack toasts that stay until closed one by one (#263).
describe('repeated request failures', () => {
  let host: HTMLElement;

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = `
      <div id="toast-region"></div>
      <template id="toast-template"><div role="alert" data-toast><span data-toast-message></span><button type="button" data-toast-close>×</button></div></template>`;
    document.body.append(host);
  });

  afterEach(() => host.remove());

  it('shows one toast for N htmx:sendError events', () => {
    for (let i = 0; i < 4; i++)
      host.dispatchEvent(new CustomEvent('htmx:sendError', { bubbles: true, detail: { xhr: { status: 0 } } }));
    expect(host.querySelectorAll('#toast-region [data-toast]').length).to.equal(1);
  });
});
