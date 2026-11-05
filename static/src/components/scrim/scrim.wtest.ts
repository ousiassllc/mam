import { aTimeout, expect, waitUntil } from '@open-wc/testing';
import { installConfirm } from '../confirm/confirm';
import { installScrim } from './scrim';

// The confirmation modal as AppLayout embeds it (molecules/modal modal.Confirm), reduced to what the script reads.
const CONFIRM =
  '<dialog data-confirm><p data-confirm-message></p><button type="button" data-confirm-cancel>キャンセル</button><button type="button" data-confirm-ok>実行する</button></dialog>';

describe('scrim', () => {
  let host: HTMLElement;
  let dialog: HTMLDialogElement;

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = `<template id="confirm-template">${CONFIRM}</template>
      <dialog id="d" style="padding:20px"><form><input name="a"><button type="button">中</button></form></dialog>`;
    document.body.append(host);
    dialog = host.querySelector('#d') as HTMLDialogElement;
    installScrim(host as unknown as Document);
    dialog.showModal();
  });
  afterEach(() => {
    dialog.close();
    host.remove();
    document.querySelectorAll('dialog[data-confirm]').forEach((d) => {
      d.remove();
    });
  });

  const clickAt = (x: number, y: number) =>
    dialog.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX: x, clientY: y }));

  it('closes on a click outside the dialog', () => {
    const box = dialog.getBoundingClientRect();
    clickAt(box.left - 5, box.top - 5);
    expect(dialog.open).to.equal(false);
  });

  it('does not close on a click on its own padding', () => {
    const box = dialog.getBoundingClientRect();
    clickAt(box.left + 3, box.top + 3);
    expect(dialog.open).to.equal(true);
  });

  it('asks before it closes a dialog whose field was changed, and keeps it open on "キャンセル"', async () => {
    installConfirm(document);
    (dialog.querySelector('input') as HTMLInputElement).dispatchEvent(new Event('input', { bubbles: true }));
    const box = dialog.getBoundingClientRect();
    clickAt(box.left - 5, box.top - 5);

    await waitUntil(() => document.querySelector('dialog[data-confirm][open]') !== null, 'no question was asked');
    expect(dialog.open).to.equal(true);
    (document.querySelector('[data-confirm-cancel]') as HTMLButtonElement).click();
    await aTimeout(20);
    expect(dialog.open).to.equal(true);
  });
});
