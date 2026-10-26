import { expect, waitUntil } from '@open-wc/testing';
import '../../main';

describe('copy button (spec/components/settings-screen-categories.md §4)', () => {
  let host: HTMLElement;
  const status = () => host.querySelector<HTMLElement>('#copy-status')!;
  const field = () => host.querySelector<HTMLInputElement>('#copy-value')!;
  const button = () => host.querySelector<HTMLButtonElement>('button')!;

  /** Replaces the clipboard of the page with `writeText` for the length of a test. */
  function useClipboard(writeText: (text: string) => Promise<void>): () => void {
    const original = Object.getOwnPropertyDescriptor(navigator, 'clipboard');
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    return () => {
      if (original) Object.defineProperty(navigator, 'clipboard', original);
      else delete (navigator as unknown as { clipboard?: unknown }).clipboard;
    };
  }

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = `
      <input id="copy-value" readonly aria-label="発行したトークン" value="mam_mcp_fake-value">
      <button type="button" data-copy-source="copy-value" data-copy-status="copy-status">コピー</button>
      <p id="copy-status" role="status"></p>`;
    document.body.append(host);
  });
  afterEach(() => host.remove());

  it('copies the value of the field and says so', async () => {
    const copied: string[] = [];
    const restore = useClipboard(async (text) => {
      copied.push(text);
    });
    try {
      button().click();
      await waitUntil(() => status().textContent === 'コピーしました', 'the line did not say it was copied');
    } finally {
      restore();
    }
    expect(copied).to.deep.equal(['mam_mcp_fake-value']);
  });

  it('selects the field and asks for a copy by hand when the clipboard refuses', async () => {
    const restore = useClipboard(() => Promise.reject(new DOMException('denied', 'NotAllowedError')));
    try {
      button().click();
      await waitUntil(() => status().textContent?.includes('手動') === true, 'the line did not ask for a manual copy');
    } finally {
      restore();
    }
    expect(document.activeElement === field()).to.equal(true);
    expect(field().selectionStart).to.equal(0);
    expect(field().selectionEnd).to.equal(field().value.length);
    expect(status().textContent).to.not.include('コピーしました');
  });

  it('works for a button that arrives after the page was loaded', async () => {
    const copied: string[] = [];
    const restore = useClipboard(async (text) => {
      copied.push(text);
    });
    try {
      host.insertAdjacentHTML(
        'beforeend',
        `<input id="late-value" readonly value="late"><button id="late" type="button" data-copy-source="late-value" data-copy-status="copy-status">コピー</button>`,
      );
      host.querySelector<HTMLButtonElement>('#late')!.click();
      await waitUntil(() => copied.length === 1, 'nothing was copied');
    } finally {
      restore();
    }
    expect(copied).to.deep.equal(['late']);
  });
});
