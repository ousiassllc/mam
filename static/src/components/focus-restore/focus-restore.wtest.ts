import { expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import { initChatWorkspaces } from '../../chat';
import '../../main';

const DATA = '/src/components/testdata';

// The buttons of a swapped region have no id, so htmx leaves the focus on <body> after the swap.
describe('focus after a swap that replaces the focused control', () => {
  let host: HTMLElement;

  beforeEach(() => {
    host = document.createElement('div');
    document.body.append(host);
  });

  afterEach(() => host.remove());

  function render(html: string): void {
    host.innerHTML = html;
    htmx.process(host);
  }

  const button = (selector: string) => host.querySelector<HTMLButtonElement>(selector)!;

  function swapped(selector: string): Promise<void> {
    const old = host.querySelector(selector);
    return waitUntil(() => host.querySelector(selector) !== old, 'the response was not swapped in', { timeout: 2000 });
  }

  it('puts the focus on the same button of the replaced switch', async () => {
    render(`
      <form id="lock-1" hx-get="${DATA}/focus-lock-after.html" hx-target="this" hx-swap="outerHTML">
        <button type="submit" name="mode" value="AUTO">AUTO</button>
        <button type="submit" name="mode" value="MANUAL">MANUAL</button>
        <button type="submit" name="mode" value="LOCK">LOCK</button>
      </form>`);
    const done = swapped('#lock-1');
    button('[value="LOCK"]').focus();
    button('[value="LOCK"]').click();
    await done;

    expect(document.activeElement).to.equal(button('[value="LOCK"]'));
    expect(document.activeElement?.getAttribute('aria-pressed')).to.equal('false');
  });

  it('moves to the same kind of button that took the place of a deleted row', async () => {
    render(`
      <ul id="rows">
        <li><button type="button" hx-get="${DATA}/focus-rows-after.html?row=a" hx-target="#rows" hx-swap="outerHTML">削除</button></li>
        <li><button type="button" id="del-b" hx-get="${DATA}/focus-rows-after.html?row=b" hx-target="#rows" hx-swap="outerHTML">削除</button></li>
        <li><button type="button" hx-get="${DATA}/focus-rows-after.html?row=c" hx-target="#rows" hx-swap="outerHTML">削除</button></li>
      </ul>`);
    const done = swapped('#rows');
    button('#del-b').focus();
    button('#del-b').click();
    await done;

    // The row after the deleted one (now second) holds the focus.
    expect(document.activeElement).to.equal(host.querySelectorAll('#rows button')[1]);
    expect(document.activeElement?.getAttribute('aria-label')).to.equal('削除 c');
  });

  it('falls back to the replaced region when no control is left in it', async () => {
    render(
      `<ul id="rows"><li><button type="button" hx-get="${DATA}/focus-rows-empty.html" hx-target="#rows" hx-swap="outerHTML">削除</button></li></ul>`,
    );
    const done = swapped('#rows');
    button('button').focus();
    button('button').click();
    await done;

    expect(document.activeElement).to.equal(host.querySelector('#rows'));
    expect(host.querySelector('#rows')?.getAttribute('tabindex')).to.equal('-1');
  });

  it('does not take the focus when the user has moved it on before the response arrives', async () => {
    render(`
      <form id="lock-1" hx-get="${DATA}/focus-lock-after.html" hx-target="this" hx-swap="outerHTML">
        <button type="submit" name="mode" value="LOCK">LOCK</button>
      </form>
      <input id="elsewhere" />`);
    const done = swapped('#lock-1');
    button('[value="LOCK"]').focus();
    button('[value="LOCK"]').click();
    host.querySelector<HTMLInputElement>('#elsewhere')!.focus();
    await done;

    expect(document.activeElement).to.equal(host.querySelector('#elsewhere'));
  });

  it('does not take the focus on a swap nobody asked for (a poll, a refresh script)', async () => {
    render(`<form id="lock-1"><button type="submit" name="mode" value="LOCK">LOCK</button></form>`);
    button('[value="LOCK"]').focus();
    // No click, submit or key: what htmx.ajax, a poll or a `load` trigger starts has no such event.
    await htmx.ajax('get', `${DATA}/focus-lock-after.html`, {
      source: '#lock-1',
      target: '#lock-1',
      swap: 'outerHTML',
    });

    expect(document.activeElement).to.equal(document.body);
  });

  it('returns the focus to the message box when the reply chip is cancelled', async () => {
    render(`
      <section id="chat-workspace" data-messages-url="/projects/p1/messages" data-ws-url="/ws/projects/p1">
        <div id="chat-thread"></div>
        <form id="chat-composer">
          <input type="hidden" id="reply-to" value="m1" />
          <div id="reply-chip" class="flex"><span id="reply-label">返信先</span><button type="button" id="reply-cancel">×</button></div>
          <textarea id="composer-content"></textarea><input id="composer-files" type="file" />
        </form>
      </section>`);
    initChatWorkspaces(host);
    await waitUntil(
      () => host.querySelector<HTMLElement>('#chat-workspace')?.dataset.chatReady === 'true',
      'not initialised',
      {
        timeout: 2000,
      },
    );
    button('#reply-cancel').focus();
    button('#reply-cancel').click();

    expect(document.activeElement).to.equal(host.querySelector('#composer-content'));
  });
});
