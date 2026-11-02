import { expect } from '@open-wc/testing';
import { initGentestInputs } from './inputs';

// The pose picker of the generation test (#986): the button next to the other pickers opens its own dialog (not the picker of the
// Project's images), the dialog closes, and a pose card that was added is marked and given back when its thumbnail is removed.
// The markup is what ComposeForm renders with the pose library (backend/internal/web/layout_fixture_test.go keeps it current).
describe('the pose picker of the generation test (spec/components/gentest-screen.md)', () => {
  let host: HTMLElement;

  const q = <T extends HTMLElement>(s: string) => host.querySelector<T>(s)!;
  const rows = () => [...host.querySelectorAll<HTMLElement>('[data-gt-list] > [data-gt-ref]')];
  const card = (ref: string) =>
    `<ul data-testid="gt-pose-results"><li data-testid="gt-pick" data-pose-card><button type="button" data-ref="${ref}">追加</button></li></ul>`;

  beforeEach(async () => {
    const html = await (await fetch('/src/components/testdata/layout-gentest-refs.html')).text();
    host = document.createElement('div');
    host.innerHTML = html;
    document.body.append(host);
    initGentestInputs(host);
  });

  afterEach(() => host.remove());

  it('opens its own dialog and closes it', () => {
    const pose = q<HTMLDialogElement>('[data-gt-pose-dialog]');
    q<HTMLButtonElement>('[data-gt-open-picker="pose"]').click();
    expect(pose.open).to.equal(true);
    expect(q<HTMLDialogElement>('[data-gt-picker-dialog]').open, 'not the picker of the images of a Project').to.equal(
      false,
    );
    pose.querySelector<HTMLButtonElement>('[data-gt-picker-close]')!.click();
    expect(pose.open).to.equal(false);
  });

  it('keeps the picker of the Project and its close button as they were', () => {
    q<HTMLButtonElement>('[data-gt-open-picker="project"]').click();
    expect(q<HTMLDialogElement>('[data-gt-picker-dialog]').open).to.equal(true);
    expect(q<HTMLDialogElement>('[data-gt-pose-dialog]').open).to.equal(false);
    q<HTMLButtonElement>('[data-gt-picker-dialog] [data-gt-picker-close]').click();
    expect(q<HTMLDialogElement>('[data-gt-picker-dialog]').open).to.equal(false);
  });

  it('marks the card that was added and gives it back when the thumbnail is removed', () => {
    const pose = q('[data-gt-pose-dialog]');
    pose.insertAdjacentHTML('beforeend', card('pose_library::std_P001_front'));
    const button = pose.querySelector<HTMLButtonElement>('button[data-ref]')!;
    pose.dispatchEvent(new CustomEvent('htmx:afterRequest', { detail: { elt: button, successful: true } }));
    expect(button.disabled).to.equal(true);
    expect(button.textContent).to.equal('追加済み');

    // the row the server answers with: the thumbnail of the pose
    q('[data-gt-list]').insertAdjacentHTML(
      'beforeend',
      rows()[0]!.outerHTML.replace(/history::r1/g, 'pose_library::std_P001_front'),
    );
    const added = rows().find((r) => r.dataset.ref === 'pose_library::std_P001_front')!;
    expect(added, 'the row is there').to.not.equal(undefined);
    expect(button.disabled, 'its thumbnail is there').to.equal(true);

    added.querySelector<HTMLButtonElement>('[data-gt-remove]')!.click();
    expect(button.disabled).to.equal(false);
    expect(button.textContent).to.equal('追加');
  });

  it('offers the role "pose" for the editing model that is ticked', () => {
    const model = host.querySelector<HTMLInputElement>('[data-model="edit"] input')!;
    model.checked = true;
    model.dispatchEvent(new Event('change', { bubbles: true }));
    const select = rows()[0]!.querySelector<HTMLSelectElement>('[data-gt-role]')!;
    const option = [...select.options].find((o) => o.value === 'pose')!;
    expect(option.disabled).to.equal(false);
    select.value = 'pose';
    expect(select.value).to.equal('pose');
  });
});
