import { expect } from '@open-wc/testing';
import { initGentestInputs } from '../inputs';

// The roles of the pictures of a video form (#1009): the first frame is where a picture starts while an image-to-video model is ticked, a role the user
// chose stays, and the server's notice is asked again when the models or the roles change. The markup is what ComposeForm renders for a video form
// (backend/internal/web/layout_fixture_test.go keeps it current).
describe('the roles of the input images of a video form (spec/components/gentest-screen.md)', () => {
  let host: HTMLElement;
  let form: HTMLElement;
  let asked: number;

  const roles = () =>
    [...host.querySelectorAll<HTMLSelectElement>('[data-gt-list] [data-gt-ref] [data-gt-role]')].map((s) => s.value);
  const tick = (id: string, on: boolean) => {
    const box = host.querySelector<HTMLInputElement>(`[data-model="${id}"] input`)!;
    box.checked = on;
    box.dispatchEvent(new Event('change', { bubbles: true }));
  };
  const choose = (index: number, role: string) => {
    const select = host.querySelectorAll<HTMLSelectElement>('[data-gt-list] [data-gt-ref] [data-gt-role]')[index]!;
    select.value = role;
    select.dispatchEvent(new Event('change', { bubbles: true }));
  };

  beforeEach(async () => {
    const html = await (await fetch('/src/components/testdata/layout-gentest-video-inputs.html')).text();
    host = document.createElement('div');
    host.innerHTML = html;
    document.body.append(host);
    form = host.querySelector<HTMLElement>('form')!;
    asked = 0;
    form.addEventListener('gt-inputs-changed', () => asked++);
    initGentestInputs(host);
  });

  afterEach(() => host.remove());

  it('starts as references while no model needs a first frame', () => {
    expect(roles()).to.deep.equal(['reference', 'reference']);
    tick('t2v', true);
    expect(roles()).to.deep.equal(['reference', 'reference']);
  });

  it('makes the first picture the first frame while an image-to-video model is ticked, and back when it is not', () => {
    tick('i2v', true);
    expect(roles()).to.deep.equal(['first_frame', 'reference']);
    tick('i2v', false);
    expect(roles()).to.deep.equal(['reference', 'reference']);
  });

  it('never moves a role the user chose', () => {
    choose(1, 'last_frame');
    tick('i2v', true);
    expect(roles()).to.deep.equal(['first_frame', 'last_frame']);
    tick('i2v', false);
    expect(roles()).to.deep.equal(['reference', 'last_frame']);
    choose(0, 'reference'); // the user's own choice of a reference is kept too
    tick('i2v', true);
    expect(roles()).to.deep.equal(['reference', 'last_frame']);
  });

  it("asks for the server's notice again when the models or a role changed, and only then", () => {
    tick('i2v', true);
    expect(asked).to.equal(1);
    tick('t2v', true); // the same roles, another model: the notice names the models
    expect(asked).to.equal(2);
    choose(1, 'last_frame');
    expect(asked).to.equal(3);
    choose(1, 'last_frame');
    expect(asked).to.equal(3);
  });
});
