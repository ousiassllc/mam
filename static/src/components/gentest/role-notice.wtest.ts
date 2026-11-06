import { expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

const DATA = '/src/components/testdata';

/** #959: the modal that says a model uses a reference as a whole picture, before the batch runs. It is the modal of the other screens. */
describe('role notice of the generation test (#959)', () => {
  let host: HTMLElement;

  const dialog = () => host.querySelector<HTMLDialogElement>('dialog')!;
  const body = () => host.querySelector<HTMLElement>('#mam-modal-body')!;
  const byTestId = (id: string) => host.querySelector<HTMLElement>(`[data-testid="${id}"]`)!;
  const buttonNamed = (name: string) =>
    [...dialog().querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.trim() === name)!;

  /** The confirmation as the server draws it; the dialog is the page's. The run button asks for the modal with a POST. */
  async function render(): Promise<void> {
    host = document.createElement('div');
    host.innerHTML = `
      <div id="confirm"></div>
      <div id="gt-batches"></div>
      <dialog id="mam-modal" data-modal aria-labelledby="mam-modal-title">
        <div id="mam-modal-body"></div>
        <p id="mam-modal-failure" role="alert" hidden></p>
        <button type="button" data-modal-close aria-label="閉じる">×</button>
      </dialog>`;
    document.body.append(host);
    host.querySelector('#confirm')!.innerHTML = await (await fetch(`${DATA}/gentest-role-confirm.html`)).text();
    htmx.process(host);
  }

  /** Opens the modal with the body the server answers to the run button: the run button's request goes to a fixture. */
  async function open(): Promise<void> {
    const run = byTestId('gt-run');
    run.setAttribute('hx-get', `${DATA}/gentest-role-notice.html`);
    run.removeAttribute('hx-post');
    htmx.process(run);
    run.focus();
    run.click();
    await waitUntil(() => dialog().open, 'the dialog did not open', { timeout: 2000 });
    await waitUntil(() => !body().inert, 'the swap did not settle', { timeout: 2000 });
  }

  beforeEach(render);
  afterEach(() => {
    if (dialog().open) dialog().close();
    host.remove();
  });

  it('has a run button that asks for the modal with the confirmation form, instead of submitting it', () => {
    const run = byTestId('gt-run');
    expect(run.getAttribute('type')).to.equal('button');
    expect(run.getAttribute('hx-post')).to.equal('/generation-test/run-notice');
    expect(run.getAttribute('hx-target')).to.equal('#mam-modal-body');
    expect(run.hasAttribute('data-modal-open')).to.equal(true);
    expect(run.closest('form')!.querySelector('input[name="estimate_id"]')).to.not.equal(null);
  });

  it('opens a modal that names each model and has no choice but "戻る" and "このまま実行"', async () => {
    await open();

    const notes = [...dialog().querySelectorAll('[data-testid="gt-role-note"]')].map((li) => li.textContent);
    expect(notes).to.deep.equal([
      'Chroma1-HD は「顔だけ」に対応していないので、全体の参照として使います。構図や服も寄ります',
      'FLUX.1 dev は「顔だけ」「画風だけ」に対応していないので、全体の参照として使います。構図や服も寄ります',
    ]);
    const choices = [...dialog().querySelectorAll('#mam-modal-content button')].map((b) => b.textContent?.trim());
    expect(choices).to.deep.equal(['戻る', 'このまま実行']);
    expect(dialog().getAttribute('aria-labelledby')).to.equal('mam-modal-title');
  });

  it('"戻る" closes the modal, runs nothing and gives the focus back to the run button', async () => {
    let posted = false;
    const onRequest = (e: Event) => {
      if ((e as CustomEvent).detail?.requestConfig?.verb === 'post') posted = true;
    };
    document.addEventListener('htmx:beforeRequest', onRequest);
    try {
      await open();
      buttonNamed('戻る').click();
      await waitUntil(() => !dialog().open, 'the dialog did not close', { timeout: 2000 });
      await waitUntil(() => body().children.length === 0, 'the body was not emptied', { timeout: 2000 });
    } finally {
      document.removeEventListener('htmx:beforeRequest', onRequest);
    }

    expect(posted).to.equal(false);
    expect(body().children.length).to.equal(0);
    expect(document.activeElement === byTestId('gt-run')).to.equal(true);
  });

  it('"このまま実行" posts the confirmed form to the run of the batch, into the batch box', async () => {
    await open();
    const sent: { path?: string; target?: string; values?: Record<string, string> } = {};
    const onRequest = (e: Event) => {
      const d = (e as CustomEvent).detail;
      if (d?.requestConfig?.verb !== 'post') return;
      sent.path = d.requestConfig.path;
      sent.target = d.target?.id;
      sent.values = d.requestConfig.parameters;
      e.preventDefault(); // nothing answers a post here
    };
    document.addEventListener('htmx:beforeRequest', onRequest);
    try {
      buttonNamed('このまま実行').click();
      await waitUntil(() => sent.path !== undefined, 'nothing was posted', { timeout: 2000 });
    } finally {
      document.removeEventListener('htmx:beforeRequest', onRequest);
    }

    expect(sent.path).to.equal('/generation-test/run');
    expect(sent.target).to.equal('gt-batches');
    expect(sent.values!['estimate_id']).to.equal('abc');
    expect(sent.values!['confirmed_total_usd']).to.equal('0');
  });
});
