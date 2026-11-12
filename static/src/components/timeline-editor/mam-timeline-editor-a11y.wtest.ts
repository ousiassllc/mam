import { expect, waitUntil } from '@open-wc/testing';
import { stubFetch } from '../testdata/fakes';
import { stubConfirm } from '../confirm/testkit';
import { API, clip, editor, order, three, timeline } from './timeline-fixtures';

describe('mam-timeline-editor: accessible names', () => {
  let stub: ReturnType<typeof stubFetch>;
  afterEach(() => stub.restore());

  it('names the buttons that repeat on every clip after the clip, starting with their visible text', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();

    const remove = [...el.querySelectorAll('[data-action="remove"]')].map((b) => b.getAttribute('aria-label'));
    expect(remove).to.deep.equal(['外す: Scene 1 · a', '外す: Scene 1 · b', '外す: Scene 1 · c']);
    expect(new Set(remove).size).to.equal(remove.length);
    expect(el.querySelector('[data-action="remove"]')?.textContent?.trim()).to.equal('外す');
  });

  it('gives every button and field the focus ring, and the small buttons a target of 24px (W3-W5)', async () => {
    stub = stubFetch({
      [`GET ${API}`]: {
        body: timeline('e1', [clip('a', 0, 4), clip('b', 0, 3, { transition: { type: 'fade', seconds: 1 } })]),
      },
    });
    const el = await editor();

    const controls = el.querySelectorAll('button, input, select, textarea');
    expect(controls.length).to.be.greaterThan(10);
    for (const control of controls) {
      expect(control.classList.contains('focus-visible:focus-ring'), control.outerHTML.slice(0, 80)).to.equal(true);
    }
    for (const action of ['up', 'down']) {
      const button = el.querySelector(`[data-action="${action}"]`)!;
      expect(button.classList.contains('min-h-6') && button.classList.contains('min-w-6')).to.equal(true);
      expect(button.hasAttribute('data-ripple')).to.equal(true);
    }
    const save = el.querySelector('[data-action="save"]')!;
    expect(save.classList.contains('min-h-8')).to.equal(true);
    expect(save.classList.contains('text-sm')).to.equal(true);
    expect(save.hasAttribute('data-ripple')).to.equal(true);
  });

  it('draws the row as a card and the added-Shot mark as a badge of at least 12px (W2, W6)', async () => {
    stub = stubFetch({
      [`GET ${API}`]: { body: timeline('e1', [clip('a', 0, 4), clip('r', 0, 2, { kind: 'insert' })]) },
    });
    const el = await editor();

    expect(el.querySelector('[data-clip="a"]')!.classList.contains('bg-surface')).to.equal(true);
    const badge = el.querySelector('[data-kind="insert"]')!;
    for (const name of ['rounded-full', 'px-2', 'py-1', 'text-xs', 'bg-info/20', 'text-info']) {
      expect(badge.classList.contains(name), name).to.equal(true);
    }
    expect(badge.className).to.not.contain('text-[10px]');
  });

  it('sets the seconds in tabular monospace digits (W7)', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();

    const digits = (selector: string) =>
      [...el.querySelectorAll(`${selector} .font-mono.tabular-nums`)].map((n) => n.textContent);
    expect(digits('[data-clip="a"] [data-start]')).to.deep.equal(['0.0', '4.0']);
    expect(digits('[data-clip="a"]')).to.include('6.0'); // Take の長さ
    expect(digits('[data-total]')).to.deep.equal(['9.0', '3']);
  });

  it('shows "未保存の変更あり" and "この段階では編集できません" as badges next to the total (W6)', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three(), { readonly: true }) } });
    const readonly = await editor();
    const warn = readonly.querySelector('[data-readonly]')!;
    for (const name of ['rounded-full', 'px-2', 'py-1', 'text-xs', 'bg-warning/20', 'text-warning']) {
      expect(warn.classList.contains(name), name).to.equal(true);
    }
    expect(readonly.querySelector('[data-dirty]')).to.equal(null);
    stub.restore();

    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();
    expect(el.querySelector('[data-dirty]')).to.equal(null);
    (el.querySelector('[data-clip="c"] [data-action="up"]') as HTMLButtonElement).click();
    await waitUntil(() => order(el).join() === 'a,c,b');
    const info = el.querySelector('[data-dirty]')!;
    expect(info.textContent).to.equal('未保存の変更あり');
    for (const name of ['rounded-full', 'px-2', 'py-1', 'text-xs', 'bg-info/20', 'text-info']) {
      expect(info.classList.contains(name), name).to.equal(true);
    }
    expect(el.querySelector('[data-total]')!.textContent).to.not.contain('未保存');
  });

  it('keeps the unsaved edits when the confirmation of "変更を戻す" is answered with キャンセル (W10)', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const confirm = stubConfirm(false);
    try {
      const el = await editor();
      (el.querySelector('[data-clip="c"] [data-action="up"]') as HTMLButtonElement).click();
      await waitUntil(() => order(el).join() === 'a,c,b');

      (el.querySelector('[data-action="revert"]') as HTMLButtonElement).click();
      await waitUntil(() => confirm.messages.length === 1);
      await el.updateComplete;

      expect(confirm.messages).to.deep.equal(['未保存の変更を破棄して、最新の版を読み直しますか？']);
      expect(order(el)).to.deep.equal(['a', 'c', 'b']);
      expect(stub.requests.filter((r) => r.method === 'GET')).to.have.length(1); // nothing was reloaded
    } finally {
      confirm.restore();
    }
  });
});
