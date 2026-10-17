import { expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';
import { expectFocusOn } from './focus-assert';

const DATA = '/src/components/testdata';

// The focus that `hx-disabled-elt` drops to <body> (Chrome does so for a button that becomes disabled), the
// areas that `hx-swap-oob` replaces, and the requests that the answer of a request sets off (`HX-Trigger`).
describe('focus after a request that disables, replaces by hx-swap-oob or sets off another one', () => {
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

  const find = <T extends HTMLElement = HTMLButtonElement>(selector: string) => host.querySelector<T>(selector)!;

  function swapped(selector: string): Promise<void> {
    const old = host.querySelector(selector);
    return waitUntil(() => host.querySelector(selector) !== old, 'the response was not swapped in', { timeout: 2000 });
  }

  describe('hx-disabled-elt', () => {
    it('returns the focus to the replaced button that disabled itself', async () => {
      render(`
        <form id="lock-1" hx-get="${DATA}/focus-lock-after.html" hx-target="this" hx-swap="outerHTML">
          <button type="submit" name="mode" value="LOCK" hx-disabled-elt="this">LOCK</button>
        </form>`);
      const done = swapped('#lock-1');
      find('button').focus();
      find('button').click();
      await done;

      expectFocusOn(find('[value="LOCK"]'));
    });

    it('returns the focus to the replaced submit button that the form disables', async () => {
      render(`
        <form id="lock-1" hx-get="${DATA}/focus-lock-after.html" hx-target="this" hx-swap="outerHTML" hx-disabled-elt="find button[type=submit]">
          <button type="submit" name="mode" value="LOCK">LOCK</button>
        </form>`);
      const done = swapped('#lock-1');
      find('button').focus();
      find('button').click();
      await done;

      expectFocusOn(find('[value="LOCK"]'));
    });

    it('gives the button back its focus when it stayed and was enabled again', async () => {
      render(
        `<button type="button" hx-get="${DATA}/focus-rows-empty.html" hx-swap="none" hx-disabled-elt="this">指示する</button>`,
      );
      find('button').focus();
      find('button').click();
      await waitUntil(() => !find('button').disabled, 'the button was not enabled again', { timeout: 2000 });
      await waitUntil(() => document.activeElement === find('button'), 'the focus did not come back', {
        timeout: 2000,
      });
    });

    it('does not take the focus when the user has moved it on while the button was disabled', async () => {
      render(`
        <form id="lock-1" hx-get="${DATA}/focus-lock-after.html" hx-target="this" hx-swap="outerHTML">
          <button type="submit" name="mode" value="LOCK" hx-disabled-elt="this">LOCK</button>
        </form>
        <input id="elsewhere" />`);
      const done = swapped('#lock-1');
      find('button').focus();
      find('button').click();
      find('#elsewhere').focus();
      await done;

      expectFocusOn(find('#elsewhere'));
    });
  });

  describe('hx-swap-oob', () => {
    it('returns the focus to the button of the stage that the response replaced, with swap none', async () => {
      render(`
        <div id="editor-actions" hx-swap="none">
          <div id="stage-a"><button type="button" hx-get="${DATA}/focus-oob-stage.html" hx-disabled-elt="this">Rough Cut を作成</button></div>
          <div id="stage-b"><button type="button" hx-get="${DATA}/focus-oob-stage.html">Editor Judge を実行</button></div>
        </div>`);
      const done = swapped('#stage-a');
      find('#stage-a button').focus();
      find('#stage-a button').click();
      await done;

      expectFocusOn(find('#stage-a button'));
      expect(document.activeElement?.textContent).to.equal('Rough Cut を作成');
    });

    it('returns the focus to the same button of the neighbouring stage when the first stage was not pressed', async () => {
      render(`
        <div id="editor-actions" hx-swap="none">
          <div id="stage-a"><button type="button" hx-get="${DATA}/focus-oob-stage.html">Rough Cut を作成</button></div>
          <div id="stage-b"><button type="button" hx-get="${DATA}/focus-oob-stage.html">Editor Judge を実行</button></div>
        </div>`);
      const done = swapped('#stage-b');
      find('#stage-b button').focus();
      find('#stage-b button').click();
      await done;

      expectFocusOn(find('#stage-b button'));
    });

    it('returns the focus to the submit button of a form outside the swap target that the response replaced', async () => {
      render(`
        <ul id="added-rows"></ul>
        <form id="add-form" hx-get="${DATA}/focus-oob-form.html" hx-target="#added-rows" hx-swap="beforeend" hx-disabled-elt="find button[type=submit]">
          <input name="title" />
          <button type="submit">追加</button>
        </form>`);
      const done = swapped('#add-form');
      find('#add-form button').focus();
      find('#add-form button').click();
      await done;

      expect(find('#added-rows').children.length).to.equal(1);
      expectFocusOn(find('#add-form button'));
    });
  });

  describe('HX-Trigger', () => {
    it('returns the focus after the reload that the answer of a click set off', async () => {
      render(`
        <article id="branch-detail" hx-get="${DATA}/focus-detail-reloaded.html" hx-trigger="branch-reload from:body" hx-target="#branch-detail" hx-swap="innerHTML">
          <div id="history-slot"><button type="button" hx-get="${DATA}/focus-trigger-after.html" hx-target="#history-slot" hx-swap="outerHTML" hx-disabled-elt="this">このバージョンに復元</button></div>
        </article>`);
      find('#history-slot button').focus();
      find('#history-slot button').click();
      await waitUntil(() => host.querySelector('#reloaded'), 'the Branch was not reloaded', { timeout: 2000 });

      expectFocusOn(find('#history-slot button'));
    });

    it('does not take the focus for a reload that nobody set off', async () => {
      render(`
        <article id="branch-detail" hx-get="${DATA}/focus-detail-reloaded.html" hx-trigger="branch-reload from:body" hx-target="#branch-detail" hx-swap="innerHTML">
          <div id="history-slot"><button type="button">このバージョンに復元</button></div>
        </article>`);
      find('#history-slot button').focus();
      document.body.dispatchEvent(new CustomEvent('branch-reload'));
      await waitUntil(() => host.querySelector('#reloaded'), 'the Branch was not reloaded', { timeout: 2000 });

      expect(document.activeElement === document.body, 'the focus is on <body>').to.equal(true);
    });
  });

  // A toggle that flips its label has no label to be found by: `data-focus-key` names it (#806, #808).
  describe('data-focus-key', () => {
    const row = (id: string, label: string) => `
      <li>
        <form hx-get="${DATA}/focus-toggle-after.html" hx-target="closest article" hx-swap="outerHTML" hx-disabled-elt="find button[type=submit]">
          <button type="submit" aria-label="${label}: ${id}" data-focus-key="resolve-${id}">${label}</button>
        </form>
      </li>`;

    it('returns the focus to the flipped toggle of the same row, not to the same-named toggle of another row', async () => {
      render(`<article id="review-1"><ul>${row('c1', '解決済みにする')}${row('c2', '解決済みにする')}</ul></article>`);
      const done = swapped('#review-1');
      find('[data-focus-key="resolve-c2"]').focus();
      find('[data-focus-key="resolve-c2"]').click();
      await done;

      expectFocusOn(find('[data-focus-key="resolve-c2"]'));
      expect(document.activeElement?.textContent).to.equal('未解決に戻す');
    });

    // The save button of a note: its note moves to the top of the list and its label changes (#809).
    it('returns the focus to the save button of the note that moved within the list', async () => {
      const save = (id: string, title: string) => `
        <form hx-get="${DATA}/focus-notes-reordered.html" hx-target="#note-list" hx-swap="outerHTML" hx-disabled-elt="find button[type=submit]">
          <button type="submit" aria-label="保存: ${title}" data-focus-key="save-${id}">保存</button>
        </form>`;
      render(`<div id="note-list">${save('n1', 'A')}${save('n2', 'B')}${save('n3', 'C')}</div>`);
      const done = swapped('#note-list');
      find('[data-focus-key="save-n3"]').focus();
      find('[data-focus-key="save-n3"]').click();
      await done;

      expectFocusOn(find('[data-focus-key="save-n3"]'));
      expect(document.activeElement?.getAttribute('aria-label')).to.equal('保存: C 新');
      expect(find('button').getAttribute('data-focus-key'), 'the note moved to the top').to.equal('save-n3');
    });
  });

  // A control of the answer that sits in a closed <details> cannot take the focus until it is opened (#810).
  describe('a control that the answer puts in a closed <details>', () => {
    const archive = (answer: string) => `
      <div id="note-list">
        <button type="button" hx-get="${DATA}/${answer}" hx-target="#note-list" hx-swap="outerHTML" hx-disabled-elt="this" data-focus-key="archive-n1">アーカイブ</button>
      </div>`;

    it('opens the <details> and returns the focus to the control in it', async () => {
      render(archive('focus-archived-in-details.html'));
      const done = swapped('#note-list');
      find('button').focus();
      find('button').click();
      await done;

      expectFocusOn(find('[data-focus-key="archive-n1"]'));
      expect(find<HTMLDetailsElement>('details').open, 'the <details> is open').to.equal(true);
    });

    it('goes on to the region when the control cannot take the focus at all', async () => {
      render(archive('focus-unfocusable-after.html'));
      const done = swapped('#note-list');
      find('button').focus();
      find('button').click();
      await done;

      expectFocusOn(find('#note-list'));
    });
  });
});
