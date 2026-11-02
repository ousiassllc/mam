import { expect } from '@open-wc/testing';
import { initGentestRecipe } from './recipe';

// The box of a recipe's name sits inside the compose form: Enter presses its own button, not the form's estimate (gentesttier/recipe.templ).
describe('the Enter key in the name of a recipe', () => {
  let host: HTMLElement;
  before(() => initGentestRecipe());
  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = `<form><div data-gt-recipe-row><input data-gt-recipe-name><button type="button" data-gt-recipe-act>保存</button></div>
      <button type="submit" id="estimate">見積り</button></form>`;
    document.body.append(host);
  });
  afterEach(() => host.remove());

  it('presses the save button and does not send the form', () => {
    const form = host.querySelector('form')!;
    let sent = false;
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      sent = true;
    });
    let pressed = 0;
    host.querySelector('[data-gt-recipe-act]')!.addEventListener('click', () => pressed++);
    const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
    host.querySelector('[data-gt-recipe-name]')!.dispatchEvent(event);
    expect(event.defaultPrevented).to.equal(true);
    expect(pressed).to.equal(1);
    expect(sent).to.equal(false);
  });

  it('leaves the other keys and the other inputs alone', () => {
    const name = host.querySelector('[data-gt-recipe-name]')!;
    const other = new KeyboardEvent('keydown', { key: 'a', bubbles: true, cancelable: true });
    name.dispatchEvent(other);
    expect(other.defaultPrevented).to.equal(false);
    const plain = document.createElement('input');
    host.querySelector('form')!.append(plain);
    const enter = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
    plain.dispatchEvent(enter);
    expect(enter.defaultPrevented).to.equal(false);
  });
});
