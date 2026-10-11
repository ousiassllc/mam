import { expect } from '@open-wc/testing';
import {
  commitSavedChange,
  installSaveOnChangeRevert,
  isSaveOnChange,
  revertUnsavedChange,
  settleSavedChange,
} from './save-on-change';

describe('save-on-change controls', () => {
  let root: HTMLElement;
  beforeEach(() => {
    root = document.createElement('div');
    document.body.append(root);
  });
  afterEach(() => root.remove());

  function modeForm(): HTMLFormElement {
    root.innerHTML = `<form hx-put="/projects/p/mode" hx-trigger="change">
      <input type="radio" name="mode" value="auto" checked>
      <input type="radio" name="mode" value="director">
    </form>`;
    return root.querySelector('form')!;
  }

  function qualitySelect(): HTMLSelectElement {
    root.innerHTML = `<select name="quality_policy" hx-put="/projects/p/quality-policy" hx-trigger="change">
      <option value="fast">Fast</option>
      <option value="balanced" selected>Balanced</option>
      <option value="max_quality">Max Quality</option>
    </select>`;
    return root.querySelector('select')!;
  }

  it('puts the radio back to the stored mode after a failed save', () => {
    const form = modeForm();
    form.querySelector<HTMLInputElement>('[value=director]')!.checked = true;
    revertUnsavedChange(form);
    expect(form.querySelector<HTMLInputElement>('[value=auto]')!.checked).to.equal(true);
    expect(form.querySelector<HTMLInputElement>('[value=director]')!.checked).to.equal(false);
  });

  it('puts the select back to the stored quality after a failed save', () => {
    const select = qualitySelect();
    select.value = 'max_quality';
    revertUnsavedChange(select);
    expect(select.value).to.equal('balanced');
  });

  it('reverts to the last saved value, not to the first drawn one', () => {
    const select = qualitySelect();
    select.value = 'fast';
    commitSavedChange(select);
    select.value = 'max_quality';
    revertUnsavedChange(select);
    expect(select.value).to.equal('fast');
  });

  it('commits a saved radio so that a later failure keeps it', () => {
    const form = modeForm();
    form.querySelector<HTMLInputElement>('[value=director]')!.checked = true;
    commitSavedChange(form);
    form.querySelector<HTMLInputElement>('[value=auto]')!.checked = true;
    revertUnsavedChange(form);
    expect(form.querySelector<HTMLInputElement>('[value=director]')!.checked).to.equal(true);
  });

  it('settles by status: 2xx commits, an error response or no response reverts', () => {
    const select = qualitySelect();
    select.value = 'fast';
    settleSavedChange(select, 204);
    for (const status of [422, 403, 500, 0]) {
      select.value = 'max_quality';
      settleSavedChange(select, status);
      expect(select.value, `status ${status}`).to.equal('fast');
    }
  });

  it('leaves a control that does not save on change alone', () => {
    root.innerHTML =
      '<form hx-post="/x"><input type="radio" name="a" value="1" checked><input type="radio" name="a" value="2"></form>';
    const form = root.querySelector('form')!;
    form.querySelector<HTMLInputElement>('[value="2"]')!.checked = true;
    expect(isSaveOnChange(form)).to.equal(false);
    revertUnsavedChange(form);
    expect(form.querySelector<HTMLInputElement>('[value="2"]')!.checked).to.equal(true);
  });

  it('reverts when htmx reports a failed request of the control (issue 538)', () => {
    installSaveOnChangeRevert(root);
    const form = modeForm();
    for (const status of [0, 403, 500]) {
      form.querySelector<HTMLInputElement>('[value=director]')!.checked = true;
      form.dispatchEvent(
        new CustomEvent('htmx:afterRequest', { bubbles: true, detail: { elt: form, xhr: { status } } }),
      );
      expect(form.querySelector<HTMLInputElement>('[value=auto]')!.checked, `status ${status}`).to.equal(true);
    }
  });
});
