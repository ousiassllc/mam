import { aTimeout, expect } from '@open-wc/testing';
import { appendAlert, createAlert, dedupeAlertsOnSwap, dismissAlertsOnClick } from './alert';

// The markup of atoms/alert.templ, as AppLayout embeds it in #alert-template.
const ALERT =
  '<div class="pointer-events-auto flex" role="alert" data-alert><p data-alert-message></p><button type="button" aria-label="閉じる" data-alert-close>×</button></div>';

describe('error alert', () => {
  let region: HTMLElement;
  let root: HTMLElement;
  let template: HTMLTemplateElement;
  beforeEach(() => {
    root = document.createElement('div');
    region = document.createElement('div');
    template = document.createElement('template');
    template.id = 'alert-template';
    template.innerHTML = ALERT;
    root.append(region, template);
    document.body.append(root);
    dismissAlertsOnClick(root);
    dedupeAlertsOnSwap(root);
  });
  afterEach(() => root.remove());

  it('is a clone of the template with only the message filled in', () => {
    const alert = createAlert('予算を超えています')!;
    expect(alert.querySelector('[data-alert-message]')!.textContent).to.equal('予算を超えています');
    alert.querySelector('[data-alert-message]')!.textContent = '';
    expect(alert.outerHTML).to.equal(template.content.firstElementChild!.outerHTML);
  });

  it('shows the message as text, not markup', () => {
    const alert = createAlert('<img src=x>')!;
    expect(alert.querySelector('img')).to.equal(null);
    expect(alert.textContent).to.contain('<img src=x>');
  });

  it('does not change the template for the next alert', () => {
    createAlert('first');
    expect(template.content.querySelector('[data-alert-message]')!.textContent).to.equal('');
  });

  it('returns null when the page has no template', () => {
    template.remove();
    expect(createAlert('x')).to.equal(null);
  });

  it('stays until the close button is pressed', async () => {
    region.append(createAlert('予算を超えています')!);
    region.append(createAlert('LOCK されています')!);
    await aTimeout(200);
    expect(region.querySelectorAll('[data-alert]').length).to.equal(2);
    expect(region.querySelector('[data-alert]')!.getAttribute('role')).to.equal('alert');

    (region.querySelector('[data-alert-close]') as HTMLButtonElement).click();
    expect(region.querySelectorAll('[data-alert]').length).to.equal(1);
    expect(region.textContent).to.contain('LOCK されています');
    expect(region.textContent).not.to.contain('予算を超えています');
  });

  it('closes a alert that the server swapped in', () => {
    region.innerHTML = ALERT;
    (region.querySelector('[data-alert-close]') as HTMLButtonElement).click();
    expect(region.children.length).to.equal(0);
  });

  it('appends a message once however often it is raised, and again after the alert was closed', () => {
    for (let i = 0; i < 5; i++) appendAlert(region, '通信に失敗しました');
    appendAlert(region, '別のエラー');
    expect(region.querySelectorAll('[data-alert]').length).to.equal(2);

    (region.querySelector('[data-alert-close]') as HTMLButtonElement).click();
    appendAlert(region, '通信に失敗しました');
    expect(Array.from(region.querySelectorAll('[data-alert-message]')).map((m) => m.textContent)).to.deep.equal([
      '別のエラー',
      '通信に失敗しました',
    ]);
  });

  it('drops a server-sent alert that the region already shows', () => {
    region.id = 'alert-region';
    appendAlert(region, 'LOCK されています');
    const oob = document.createElement('div');
    for (const message of ['LOCK されています', '予算を超えています', '予算を超えています'])
      oob.append(createAlert(message)!);
    root.dispatchEvent(
      new CustomEvent('htmx:oobBeforeSwap', { bubbles: true, detail: { target: region, fragment: oob } }),
    );
    expect(Array.from(oob.querySelectorAll('[data-alert-message]')).map((m) => m.textContent)).to.deep.equal([
      '予算を超えています',
    ]);
  });
});
