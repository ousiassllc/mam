import { expect } from '@open-wc/testing';
import { initLoRAImport } from './lora-import';

// The "ファイルを追加" form of the model manager (#1119): a chosen or dropped file is checked in the browser (the server checks it all again) and sent only
// when it can be; the progress of the transfer is shown. The data attributes are those of organisms/modelmgr/loraimport (its Go test checks the page has them).
const FORM = `
<div data-max-bytes="1048576">
  <form data-lora-pick>
    <input type="file" name="file" data-lora-file>
    <div data-lora-zone>ここにドロップ</div>
    <p data-lora-error hidden></p>
    <div data-lora-progress hidden><progress max="100" value="0"></progress><p data-lora-progress-text></p></div>
    <button type="submit">send</button>
  </form>
</div>`;

const file = (name: string, size: number) => new File([new Uint8Array(size)], name);

describe('the "ファイルを追加" form of the model manager (#1119)', () => {
  let host: HTMLElement;
  let submitted: number;
  const q = <T extends Element>(selector: string) => host.querySelector<T>(selector) as T;
  const form = () => q<HTMLFormElement>('[data-lora-pick]');

  function choose(f: File): void {
    const files = new DataTransfer();
    files.items.add(f);
    q<HTMLInputElement>('[data-lora-file]').files = files.files;
    q('[data-lora-file]').dispatchEvent(new Event('change', { bubbles: true }));
  }

  function drop(f: File): Event {
    const data = new DataTransfer();
    data.items.add(f);
    const event = new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: data });
    q('[data-lora-zone]').dispatchEvent(event);
    return event;
  }

  before(() => initLoRAImport());

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = FORM;
    document.body.append(host);
    submitted = 0;
    form().addEventListener('submit', (e) => {
      e.preventDefault();
      submitted++;
    });
  });
  afterEach(() => host.remove());

  it('sends a .safetensors file that was chosen, and shows the progress bar at 0', () => {
    choose(file('a.safetensors', 100));
    expect(submitted).to.equal(1);
    expect(q<HTMLElement>('[data-lora-error]').hidden).to.equal(true);
    expect(q<HTMLElement>('[data-lora-progress]').hidden).to.equal(false);
  });

  it('does not send a file of another kind, or one over the limit, and says why', () => {
    choose(file('a.ckpt', 100));
    expect(submitted).to.equal(0);
    expect(q('[data-lora-error]').textContent).to.contain('.safetensors のファイルを選んでください');
    expect(q<HTMLInputElement>('[data-lora-file]').files?.length).to.equal(0);
    choose(file('big.safetensors', 2 << 20));
    expect(submitted).to.equal(0);
    expect(q('[data-lora-error]').textContent).to.contain('ファイルが大きすぎます');
  });

  it('takes a file dropped on the zone as the chosen one', () => {
    const event = drop(file('dropped.safetensors', 50));
    expect(event.defaultPrevented).to.equal(true);
    expect(q<HTMLInputElement>('[data-lora-file]').files?.[0]?.name).to.equal('dropped.safetensors');
    expect(submitted).to.equal(1);
  });

  it('shows the progress of the transfer that htmx reports', () => {
    q('[data-lora-file]').dispatchEvent(
      new CustomEvent('htmx:xhr:progress', {
        bubbles: true,
        detail: { loaded: 512 * 1024, total: 1 << 20, lengthComputable: true },
      }),
    );
    expect(q<HTMLProgressElement>('progress').value).to.equal(50);
    expect(q('[data-lora-progress-text]').textContent).to.contain('50%');
  });
});
