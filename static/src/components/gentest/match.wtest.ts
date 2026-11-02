import { expect } from '@open-wc/testing';
import { initGentestMatch } from './match';

// The prompt match form of the generation test (#916): the chosen image is shown in the drop zone, the target and the limit of rounds
// are shown only for the way that repeats on its own, and the slider writes its value beside it. The data attributes are those of
// MatchForm (backend/internal/web/handler/gentest/matchscreen checks the page still has them).
const FORM = `
<form data-gt-match data-max-bytes="1048576">
  <input type="file" name="upload" data-gt-match-file>
  <label data-gt-match-zone>
    <img data-gt-match-preview alt="" hidden>
    <span data-gt-match-hint>画像をドロップ、またはクリック</span>
  </label>
  <input type="hidden" name="ref" value="history::abc" data-gt-match-ref>
  <p data-gt-match-error hidden></p>
  <input type="radio" name="mode" value="auto" data-gt-match-mode checked>
  <input type="radio" name="mode" value="once" data-gt-match-mode>
  <div data-gt-match-auto><output data-gt-match-target-output>80%</output><input type="range" data-gt-match-target min="50" max="100" value="80"></div>
</form>`;

const pngFile = (size = 10, type = 'image/png') => new File([new Uint8Array(size)], 'ref.png', { type });

function choose(input: HTMLInputElement, file: File): void {
  const files = new DataTransfer();
  files.items.add(file);
  input.files = files.files;
  input.dispatchEvent(new Event('change', { bubbles: true }));
}

describe('the prompt match form of the generation test (spec/components/gentest-screen.md)', () => {
  let host: HTMLElement;
  const q = <T extends Element>(selector: string) => host.querySelector<T>(selector)!;

  before(() => initGentestMatch());

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = FORM;
    document.body.append(host);
  });

  afterEach(() => host.remove());

  it('shows the chosen image in the drop zone and stops naming the image of the link it was opened with', () => {
    choose(q('[data-gt-match-file]'), pngFile());
    const preview = q<HTMLImageElement>('[data-gt-match-preview]');
    expect(preview.hidden).to.equal(false);
    expect(preview.src.startsWith('blob:')).to.equal(true);
    expect(q<HTMLElement>('[data-gt-match-hint]').hidden).to.equal(true);
    expect(q<HTMLInputElement>('[data-gt-match-ref]').value, 'the file is the reference now').to.equal('');
    expect(q<HTMLElement>('[data-gt-match-error]').hidden).to.equal(true);
  });

  it('refuses a file that is not a PNG, JPEG or WebP, or is too large, and says why in one line', () => {
    const input = q<HTMLInputElement>('[data-gt-match-file]');
    choose(input, pngFile(10, 'image/gif'));
    expect(q<HTMLElement>('[data-gt-match-error]').hidden).to.equal(false);
    expect(q<HTMLElement>('[data-gt-match-error]').textContent).to.contain('PNG・JPEG・WebP');
    expect(input.files!.length, 'the refused file is not kept').to.equal(0);
    expect(q<HTMLImageElement>('[data-gt-match-preview]').hidden).to.equal(true);

    choose(input, pngFile((1 << 20) + 1));
    expect(q<HTMLElement>('[data-gt-match-error]').textContent).to.contain('1 MiB');
    choose(input, pngFile());
    expect(q<HTMLElement>('[data-gt-match-error]').hidden, 'a good file clears the line').to.equal(true);
  });

  it('shows the target and the limit of rounds only for the way that repeats on its own', () => {
    const limits = q<HTMLElement>('[data-gt-match-auto]');
    const once = q<HTMLInputElement>('input[value=once]');
    once.checked = true;
    once.dispatchEvent(new Event('change', { bubbles: true }));
    expect(limits.hidden).to.equal(true);
    const auto = q<HTMLInputElement>('input[value=auto]');
    auto.checked = true;
    auto.dispatchEvent(new Event('change', { bubbles: true }));
    expect(limits.hidden).to.equal(false);
  });

  it('writes the value of the slider beside it as it moves', () => {
    const slider = q<HTMLInputElement>('[data-gt-match-target]');
    slider.value = '95';
    slider.dispatchEvent(new Event('input', { bubbles: true }));
    expect(q<HTMLElement>('[data-gt-match-target-output]').textContent).to.equal('95%');
  });
});
