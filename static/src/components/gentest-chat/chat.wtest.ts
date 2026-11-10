import { expect } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// #1138: the chat of the single generation. The page is what the handlers render (backend/internal/web/gentest_chat_fixture_test.go): the panel, and after it the
// entries of a talk, which the test appends to the thread as htmx does (hx-swap="beforeend") and then tells the document the swap happened.
const TESTDATA = '/src/components/testdata/';

describe('the chat of the single generation (#1138)', () => {
  let host: HTMLElement;

  const thread = () => host.querySelector<HTMLElement>('#gtc-thread')!;
  const box = () => host.querySelector<HTMLTextAreaElement>('#gtc-content')!;
  const composer = () => host.querySelector<HTMLFormElement>('#gtc-composer')!;

  async function show(): Promise<void> {
    host = document.createElement('div');
    host.innerHTML = await (await fetch(`${TESTDATA}gentest-chat-thread.html`)).text();
    document.body.append(host);
    htmx.process(host);
    const panel = host.querySelector('section[data-gtc]')!;
    const entries = [...host.children].filter((el) => el !== panel);
    thread().append(...entries);
    thread().dispatchEvent(new Event('htmx:afterSwap', { bubbles: true }));
  }

  /** Counts the submissions of the composer without sending them anywhere. */
  function countSubmits(): { count: () => number } {
    let n = 0;
    host.addEventListener(
      'submit',
      (event) => {
        event.preventDefault();
        event.stopPropagation(); // before htmx sees it: nothing is posted
        n += 1;
      },
      true,
    );
    return { count: () => n };
  }

  const key = (init: KeyboardEventInit) =>
    box().dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init }));

  afterEach(() => host.remove());

  it('sends on Enter, breaks the line on Shift+Enter, and waits for an IME conversion', async () => {
    await show();
    const submits = countSubmits();
    box().value = '狐を作りたい';

    key({ key: 'Enter', shiftKey: true });
    expect(submits.count(), 'Shift+Enter is a line break').to.equal(0);
    key({ key: 'Enter', isComposing: true });
    expect(submits.count(), 'the Enter that confirms a conversion is not a send').to.equal(0);

    key({ key: 'Enter' });
    expect(submits.count()).to.equal(1);

    box().value = '   ';
    key({ key: 'Enter' });
    expect(submits.count(), 'a blank message is not sent').to.equal(1);
  });

  it('empties the box once the message was taken, and keeps the words of a refused one', async () => {
    await show();
    box().value = '狐を作りたい';
    composer().dispatchEvent(
      new CustomEvent('htmx:afterRequest', {
        bubbles: true,
        detail: { elt: composer(), successful: false, xhr: { status: 422 } },
      }),
    );
    expect(box().value, 'a refused message keeps its words').to.equal('狐を作りたい');
    composer().dispatchEvent(
      new CustomEvent('htmx:afterRequest', {
        bubbles: true,
        detail: { elt: composer(), successful: true, xhr: { status: 200 } },
      }),
    );
    expect(box().value).to.equal('');
  });

  it('turns off the cards of a question that was answered and keeps the newest question live', async () => {
    await show();
    const groups = [...host.querySelectorAll<HTMLElement>('[data-gtc-options]')];
    expect(groups.length, 'the greeting and the question of the talk').to.equal(2);
    expect(groups[0]!.inert, 'the greeting was answered by the talk').to.equal(true);
    expect(groups[0]!.hasAttribute('data-answered')).to.equal(true);
    expect(groups[1]!.inert, 'the newest question is live').to.equal(false);
  });

  it('shows the confirmation card with the total, the breakdown, and a button that carries only the conversation', async () => {
    await show();
    const card = host.querySelector<HTMLElement>('[data-testid="gtc-proposal"]')!;
    expect(card.querySelector('[data-testid="gtc-total"]')!.textContent).to.equal('$0.1272');
    expect(card.querySelectorAll('[data-testid="gtc-line"]').length).to.equal(2);
    const run = card.querySelector<HTMLFormElement>('form[hx-post$="/confirm"]')!;
    expect([...run.querySelectorAll('input')].map((i) => i.name)).to.deep.equal(['session']);
    expect(run.querySelector('[data-testid="gtc-run"]')!.textContent).to.contain('この内容で実行する');
  });

  it('draws the results as tiles that link to the result', async () => {
    await show();
    const tiles = host.querySelectorAll('[data-testid="gtc-tiles"] [data-testid="gt-card"]');
    expect(tiles.length).to.equal(2);
    expect(
      host.querySelector('[data-testid="gtc-result"]')!.hasAttribute('hx-get'),
      'a finished batch does not poll',
    ).to.equal(false);
  });
});
