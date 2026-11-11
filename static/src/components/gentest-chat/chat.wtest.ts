import { expect } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// #1138: the chat of the single generation. The page is what the handlers render (backend/internal/web/organisms/gentest/chat/fixture_test.go): the panel, and after it the
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

  afterEach(() => host?.remove()); // a failed fetch in show() leaves host undefined: do not hide that error behind a TypeError

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

  const request = (name: 'htmx:beforeRequest' | 'htmx:afterRequest', status?: number) =>
    composer().dispatchEvent(
      new CustomEvent(name, {
        bubbles: true,
        detail: { elt: composer(), successful: true, ...(status === undefined ? {} : { xhr: { status } }) },
      }),
    );

  it('empties the box once the message was taken, and keeps the words of a refused one', async () => {
    await show();
    box().value = '狐を作りたい';
    request('htmx:beforeRequest');
    request('htmx:afterRequest', 422);
    expect(box().value, 'a swapped 422 (htmx says successful) keeps the words').to.equal('狐を作りたい');
    request('htmx:beforeRequest');
    request('htmx:afterRequest', 200);
    expect(box().value).to.equal('');
  });

  it('empties only what was sent: the next message typed while the request ran is kept', async () => {
    await show();
    box().value = '狐を作りたい';
    request('htmx:beforeRequest');
    box().value = '狐を作りたい。次は猫も';
    request('htmx:afterRequest', 200);
    expect(box().value, 'a 2xx answer does not take the message typed meanwhile').to.equal('狐を作りたい。次は猫も');

    request('htmx:beforeRequest');
    request('htmx:afterRequest', 200);
    expect(box().value, 'the words that were sent are cleared').to.equal('');
  });

  describe('after a tap on an option card', () => {
    /** The form of the first card of the newest question, as htmx reports its request: the button was disabled by hx-disabled-elt, so the focus is on <body>. */
    function tapped(): HTMLFormElement {
      const group = [...host.querySelectorAll<HTMLElement>('[data-gtc-options]')].pop()!;
      const form = group.querySelector<HTMLFormElement>('form')!;
      form.querySelector('button')!.focus();
      (document.activeElement as HTMLElement).blur();
      return form;
    }
    const finished = (form: HTMLFormElement, status: number) =>
      form.dispatchEvent(
        new CustomEvent('htmx:afterRequest', {
          bubbles: true,
          detail: { elt: form, successful: true, xhr: { status } },
        }),
      );

    it('moves the focus to the box, where the focus of a sent message goes', async () => {
      await show();
      const form = tapped();
      expect(document.activeElement === document.body, 'the focus fell to the body').to.equal(true);

      finished(form, 200);

      expect(document.activeElement === box(), 'the box has the focus').to.equal(true);
    });

    it('leaves the focus where the user put it, and a refused answer does not take it', async () => {
      await show();
      const elsewhere = document.createElement('button');
      host.append(elsewhere);
      const form = tapped();
      elsewhere.focus();
      finished(form, 200);
      expect(document.activeElement === elsewhere, 'the user moved on').to.equal(true);

      (document.activeElement as HTMLElement).blur();
      finished(form, 422);
      expect(document.activeElement === document.body, 'a refusal keeps the cards as they are').to.equal(true);
    });
  });

  describe('the thread follows the conversation', () => {
    const swap = (target: Element, name: 'htmx:beforeSwap' | 'htmx:afterSwap') =>
      target.dispatchEvent(
        new CustomEvent(name, {
          bubbles: true,
          detail: { xhr: { status: 200, getResponseHeader: () => 'text/html' }, shouldSwap: true, isError: false },
        }),
      );

    /** What the 2-second polling does to the result block: the block is replaced by a new one of the same place (outerHTML). */
    function pollResult(): void {
      const old = host.querySelector<HTMLElement>('[data-testid="gtc-result"]')!;
      swap(old, 'htmx:beforeSwap');
      const fresh = old.cloneNode(true) as HTMLElement;
      fresh.style.minHeight = `${old.offsetHeight + 40}px`; // the block grew
      old.replaceWith(fresh);
      swap(fresh, 'htmx:afterSwap');
    }

    async function scrollable(): Promise<void> {
      await show();
      thread().style.maxHeight = '120px';
      thread().style.overflowY = 'auto';
      expect(thread().scrollHeight, 'the thread overflows').to.be.greaterThan(thread().clientHeight + 200);
    }

    it('leaves a reader who scrolled up where they are when the result block redraws itself', async () => {
      await scrollable();
      thread().scrollTop = 0;

      pollResult();

      expect(thread().scrollTop).to.equal(0);
    });

    it('stays at the bottom for a reader who is there when the result block redraws itself', async () => {
      await scrollable();
      thread().scrollTop = thread().scrollHeight;

      pollResult();

      expect(thread().scrollHeight - thread().scrollTop - thread().clientHeight).to.be.at.most(1);
    });

    it('goes to a new entry even when the reader scrolled up', async () => {
      await scrollable();
      thread().scrollTop = 0;

      swap(thread(), 'htmx:beforeSwap');
      const entry = document.createElement('article');
      entry.style.minHeight = '100px';
      thread().append(entry);
      swap(thread(), 'htmx:afterSwap');

      expect(thread().scrollHeight - thread().scrollTop - thread().clientHeight).to.be.at.most(1);
    });
  });

  it('turns off the cards of the questions that were answered', async () => {
    await show();
    const groups = [...host.querySelectorAll<HTMLElement>('[data-gtc-options]')];
    expect(groups.length, 'the greeting and the question of the talk').to.equal(2);
    expect(groups[0]!.inert, 'the greeting was answered by the talk').to.equal(true);
    expect(groups[0]!.hasAttribute('data-answered')).to.equal(true);
    expect(groups[1]!.inert, 'the user answered the question with the next entry').to.equal(true);
  });

  it('keeps the newest question live until the user answers it, even when the assistant then comes back without cards', async () => {
    await show();
    const groups = [...host.querySelectorAll<HTMLElement>('[data-gtc-options]')];
    // the talk as it was before the user answered the question: its answer is not in the thread yet
    for (const g of groups) {
      g.inert = false;
      g.removeAttribute('data-answered');
    }
    const answers = host.querySelectorAll('[data-testid="gtc-user"]');
    answers[answers.length - 1]!.remove();
    thread().dispatchEvent(new Event('htmx:afterSwap', { bubbles: true }));
    expect(groups[0]!.inert, 'the greeting was answered by the first message').to.equal(true);
    expect(groups[1]!.inert, 'the newest question is live').to.equal(false);

    // the answer comes as words, and the assistant replies without cards
    const user = document.createElement('article');
    user.setAttribute('data-testid', 'gtc-user');
    const reply = document.createElement('article');
    reply.setAttribute('data-testid', 'gtc-assistant');
    thread().append(user, reply);
    thread().dispatchEvent(new Event('htmx:afterSwap', { bubbles: true }));
    expect(groups[1]!.inert, 'the answered question is off although no newer cards exist').to.equal(true);
    expect(groups[1]!.hasAttribute('data-answered')).to.equal(true);
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
