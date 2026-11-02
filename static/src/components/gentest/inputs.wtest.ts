import { expect } from '@open-wc/testing';
import { setViewport } from '@web/test-runner-commands';
import { initGentestInputs } from './inputs';

// The input images of the generation test (#933): the thumbnails with their numbers, remove, reorder, the roles and the
// strengths that follow the chosen models, and the images chosen in the browser. The markup is what ComposeForm renders
// (backend/internal/web/layout_fixture_test.go keeps it current), drawn with the real Tailwind CSS.
describe('the input images of the generation test (spec/components/gentest-screen.md)', () => {
  let host: HTMLElement;
  let stylesheet: HTMLLinkElement;

  const q = <T extends HTMLElement>(s: string) => host.querySelector<T>(s)!;
  const rows = () => [...host.querySelectorAll<HTMLElement>('[data-gt-list] > [data-gt-ref]')];
  const refs = () => rows().map((r) => r.dataset.ref);
  const badges = () => rows().map((r) => r.querySelector('[data-gt-badge]')!.textContent);
  const model = (id: string) => host.querySelector<HTMLInputElement>(`[data-model="${id}"] input`)!;
  const tick = (id: string, on: boolean) => {
    model(id).checked = on;
    model(id).dispatchEvent(new Event('change', { bubbles: true }));
  };
  const png = (name: string) => new File([new Uint8Array(8)], name, { type: 'image/png' });
  const choose = (...files: File[]) => {
    const dt = new DataTransfer();
    for (const f of files) dt.items.add(f);
    const input = q<HTMLInputElement>('[data-gt-file]');
    input.files = dt.files;
    input.dispatchEvent(new Event('change', { bubbles: true }));
  };

  beforeEach(async function () {
    this.timeout(60_000);
    stylesheet = document.createElement('link');
    stylesheet.rel = 'stylesheet';
    stylesheet.href = '/__tailwind.css';
    const loaded = new Promise<void>((resolve, reject) => {
      stylesheet.onload = () => resolve();
      stylesheet.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
    });
    document.head.append(stylesheet);
    await loaded;
    await setViewport({ width: 800, height: 900 });
    const html = await (await fetch('/src/components/testdata/layout-gentest-refs.html')).text();
    host = document.createElement('div');
    host.style.padding = '16px';
    host.innerHTML = html;
    document.body.append(host);
    initGentestInputs(host);
  });

  afterEach(() => {
    host.remove();
    stylesheet.remove();
  });

  it('numbers the thumbnails in order and counts them against the limit', () => {
    expect(badges()).to.deep.equal(['1', '2', '3']);
    expect(q('[data-gt-count]').textContent).to.equal('3 / 10');
  });

  it('removes an image and renumbers the rest', () => {
    rows()[0]!.querySelector<HTMLButtonElement>('[data-gt-remove]')!.click();
    expect(refs()).to.deep.equal(['history::r2', 'history::r3']);
    expect(badges()).to.deep.equal(['1', '2']);
    expect(q('[data-gt-count]').textContent).to.equal('2 / 10');
  });

  it('moves an image with the buttons (the keyboard way) and keeps the focus on the button', () => {
    const second = rows()[1]!.querySelector<HTMLButtonElement>('[data-gt-move="-1"]')!;
    second.click();
    expect(refs()).to.deep.equal(['history::r2', 'history::r1', 'history::r3']);
    expect(badges()).to.deep.equal(['1', '2', '3']);
    expect(document.activeElement).to.equal(rows()[0]!.querySelector('[data-gt-move="-1"]'));
    rows()[2]!.querySelector<HTMLButtonElement>('[data-gt-move="1"]')!.click();
    expect(refs(), 'the last one cannot move further').to.deep.equal(['history::r2', 'history::r1', 'history::r3']);
  });

  it('moves an image by dragging it to another', () => {
    const [first, , third] = rows();
    const dt = new DataTransfer();
    first!.dispatchEvent(new DragEvent('dragstart', { bubbles: true, dataTransfer: dt }));
    const rect = third!.getBoundingClientRect();
    third!.dispatchEvent(
      new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, clientX: rect.right - 1 }),
    );
    third!.dispatchEvent(
      new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, clientX: rect.right - 1 }),
    );
    expect(refs()).to.deep.equal(['history::r2', 'history::r3', 'history::r1']);
    expect(badges()).to.deep.equal(['1', '2', '3']);
  });

  it('shows the roles that the chosen models take, and the strength beside each, from the model cards', () => {
    const select = rows()[0]!.querySelector<HTMLSelectElement>('[data-gt-role]')!;
    const offered = () => [...select.options].filter((o) => !o.disabled).map((o) => o.value);
    expect(offered(), 'the editing model takes all four').to.deep.equal(['reference', 'face', 'style', 'edit']);
    expect(rows()[0]!.querySelector('input[type=range]')).to.not.equal(null);

    tick('edit', false);
    tick('i2i', true);
    expect(offered(), 'an img2img model takes the whole picture only').to.deep.equal(['reference']);
    expect(
      rows()[1]!.querySelector<HTMLSelectElement>('[data-gt-role]')!.value,
      'a role it does not take is put back',
    ).to.equal('reference');

    tick('i2i', false);
    tick('plain', true);
    const controls = rows()[0]!.querySelector<HTMLElement>('[data-gt-ref-controls]')!;
    expect(controls.hidden, 'a model that takes none: no role or strength').to.equal(true);
    expect(controls.querySelector<HTMLInputElement>('input[type=range]')!.disabled, 'and they post nothing').to.equal(
      true,
    );
  });

  it('tells at once that a chosen model does not use the images', () => {
    const notice = q('[data-gt-noref]');
    expect(notice.hidden).to.equal(true);
    tick('plain', true);
    expect(notice.hidden).to.equal(false);
    expect(notice.textContent).to.equal('FLUX.1 schnell は入力画像を使いません');
    tick('plain', false);
    expect(notice.hidden).to.equal(true);
  });

  it('shows the pose option and the LoRA only while a chosen model can use them', () => {
    expect(q('[data-gt-avoid]').hidden).to.equal(false);
    expect(q('[data-gt-lora]').hidden, 'the editing model takes a LoRA').to.equal(false);
    tick('edit', false);
    tick('i2i', true);
    expect(q('[data-gt-avoid]').hidden).to.equal(false);
    expect(q('[data-gt-lora]').hidden, 'the img2img model does not').to.equal(true);
    expect(q<HTMLSelectElement>('[data-gt-lora] select').disabled).to.equal(true);
    tick('i2i', false);
    expect(q('[data-gt-avoid]').hidden).to.equal(true);
    expect(q<HTMLInputElement>('[data-gt-avoid] input').disabled).to.equal(true);
  });

  it('shows the value of a strength slider as it moves', () => {
    const slider = rows()[0]!.querySelector<HTMLInputElement>('input[type=range]')!;
    expect(slider.nextElementSibling!.textContent).to.equal('0.70');
    slider.value = '0.35';
    slider.dispatchEvent(new Event('input', { bubbles: true }));
    expect(slider.nextElementSibling!.textContent).to.equal('0.35');
  });

  it('adds the images chosen in the browser, names their fields after their number among the files and keeps the files in order', () => {
    choose(png('a.png'), png('b.png'));
    expect(rows()).to.have.length(5);
    expect(refs().slice(3)).to.deep.equal(['upload::#1', 'upload::#2']);
    const added = rows()[4]!;
    expect(added.querySelector<HTMLInputElement>('input[name="ref"]')!.value).to.equal('upload::#2');
    expect(added.querySelector('[data-gt-role]')!.getAttribute('name')).to.equal('role_upload::#2');
    expect(added.querySelector('[data-gt-strength]')!.getAttribute('name')).to.equal('strength_upload::#2');
    expect(added.querySelector('img')!.src, 'a real thumbnail, not a placeholder').to.match(/^blob:/);
    expect([...q<HTMLInputElement>('[data-gt-file]').files!].map((f) => f.name)).to.deep.equal(['a.png', 'b.png']);

    added.querySelector<HTMLButtonElement>('[data-gt-move="-1"]')!.click();
    expect(refs().slice(3)).to.deep.equal(['upload::#1', 'upload::#2']);
    expect(
      [...q<HTMLInputElement>('[data-gt-file]').files!].map((f) => f.name),
      'the files follow the order',
    ).to.deep.equal(['b.png', 'a.png']);

    rows()[3]!.querySelector<HTMLButtonElement>('[data-gt-remove]')!.click();
    expect([...q<HTMLInputElement>('[data-gt-file]').files!].map((f) => f.name)).to.deep.equal(['a.png']);
    expect(refs().slice(3)).to.deep.equal(['upload::#1']);
  });

  it('says in one line that the limit is reached, and does not add more', () => {
    choose(...Array.from({ length: 9 }, (_, i) => png(`${i}.png`)));
    expect(rows()).to.have.length(10);
    const error = q('[data-gt-error]');
    expect(error.hidden).to.equal(false);
    expect(error.textContent).to.equal('最大 10 枚です');
    expect(q('[data-gt-count]').textContent).to.equal('10 / 10');
  });

  it('opens the picker and the lightbox as dialogs', () => {
    q<HTMLButtonElement>('[data-gt-open-picker="project"]').click();
    expect(q<HTMLDialogElement>('[data-gt-picker-dialog]').open).to.equal(true);
    q<HTMLButtonElement>('[data-gt-picker-close]').click();
    expect(q<HTMLDialogElement>('[data-gt-picker-dialog]').open).to.equal(false);

    rows()[0]!.querySelector<HTMLButtonElement>('[data-gt-zoom]')!.click();
    expect(q<HTMLDialogElement>('[data-gt-lightbox]').open).to.equal(true);
    expect(q<HTMLImageElement>('[data-gt-lightbox-image]').src).to.equal(rows()[0]!.querySelector('img')!.src);
    q<HTMLButtonElement>('[data-gt-lightbox-close]').click();
    expect(q<HTMLDialogElement>('[data-gt-lightbox]').open).to.equal(false);
  });

  it('keeps a file name with replacement patterns as it is in the labels of its thumbnail', () => {
    choose(png('a$&b.png'));
    const labels = [...rows()[3]!.querySelectorAll('[aria-label],[title]')].flatMap((el) => [
      el.getAttribute('aria-label') ?? '',
      el.getAttribute('title') ?? '',
    ]);
    expect(labels.some((l) => l.includes('a$&b.png'))).to.equal(true);
    expect(labels.some((l) => l.includes('__NAME__'))).to.equal(false);
  });

  it('does not let the browser open an image dropped on the form, and adds one dropped on the thumbnails', () => {
    const drop = (target: Element, type: string, files: File[]) => {
      const dt = new DataTransfer();
      for (const f of files) dt.items.add(f);
      const e = new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt });
      target.dispatchEvent(e);
      return e;
    };
    const prompt = q('textarea');
    expect(drop(prompt, 'dragover', [png('p.png')]).defaultPrevented, 'the drop is allowed').to.equal(true);
    expect(drop(prompt, 'drop', [png('p.png')]).defaultPrevented, 'the browser does not navigate').to.equal(true);
    expect(rows(), 'a drop on the prompts adds nothing').to.have.length(3);

    expect(drop(rows()[0]!, 'drop', [png('t.png')]).defaultPrevented).to.equal(true);
    expect(rows(), 'a drop on the thumbnails adds the image').to.have.length(4);
    expect(rows()[3]!.dataset.ref).to.equal('upload::#1');

    const plain = new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: new DataTransfer() });
    prompt.dispatchEvent(plain);
    expect(plain.defaultPrevented, 'a drag of text is left alone').to.equal(false);
  });

  it('keeps the drop highlight while the drag moves over what is inside the zone', () => {
    const zone = q('[data-gt-dropzone]');
    const dt = new DataTransfer();
    dt.items.add(png('p.png'));
    zone.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt }));
    expect(zone.classList.contains('bg-indigo-500/20')).to.equal(true);
    const inner = zone.querySelector('button,label,span') ?? zone.firstElementChild!;
    zone.dispatchEvent(new DragEvent('dragleave', { bubbles: true, relatedTarget: inner }));
    expect(zone.classList.contains('bg-indigo-500/20'), 'moving onto a child is not leaving').to.equal(true);
    zone.dispatchEvent(new DragEvent('dragleave', { bubbles: true, relatedTarget: document.body }));
    expect(zone.classList.contains('bg-indigo-500/20')).to.equal(false);
  });

  it('gives the picker button back when its thumbnail is removed', () => {
    const dialog = q('[data-gt-picker-dialog]');
    dialog.insertAdjacentHTML(
      'beforeend',
      '<ul><li data-testid="gt-pick"><button type="button" data-ref="history::r1" disabled>追加済み</button></li></ul>',
    );
    const button = dialog.querySelector<HTMLButtonElement>('button[data-ref]')!;
    rows()[1]!.querySelector<HTMLButtonElement>('[data-gt-remove]')!.click();
    expect(button.disabled, 'r1 is still among the thumbnails').to.equal(true);
    rows()[0]!.querySelector<HTMLButtonElement>('[data-gt-remove]')!.click();
    expect(button.disabled).to.equal(false);
    expect(button.textContent).to.equal('追加');
  });

  it('moves the focus to the neighbouring thumbnail, or to the file button, after a removal', () => {
    rows()[1]!.querySelector<HTMLButtonElement>('[data-gt-remove]')!.click();
    expect(document.activeElement).to.equal(rows()[1]!.querySelector('[data-gt-remove]'));
    rows()[1]!.querySelector<HTMLButtonElement>('[data-gt-remove]')!.click();
    expect(document.activeElement, 'the one before it when it was the last').to.equal(
      rows()[0]!.querySelector('[data-gt-remove]'),
    );
    rows()[0]!.querySelector<HTMLButtonElement>('[data-gt-remove]')!.click();
    expect(document.activeElement).to.equal(q('[data-gt-file]'));
  });

  it('lets go of the object URLs of the browser-chosen files when the form is swapped away', () => {
    const revoked: string[] = [];
    const original = URL.revokeObjectURL;
    URL.revokeObjectURL = (u: string) => {
      revoked.push(u);
    };
    try {
      choose(png('a.png'), png('b.png'));
      const urls = rows()
        .slice(3)
        .map((r) => r.querySelector('img')!.src);
      expect(urls).to.have.length(2);
      q('[data-gt-inputs]').dispatchEvent(new CustomEvent('htmx:beforeCleanupElement', { bubbles: true }));
      expect(revoked).to.include.members(urls);
    } finally {
      URL.revokeObjectURL = original;
    }
  });
});
