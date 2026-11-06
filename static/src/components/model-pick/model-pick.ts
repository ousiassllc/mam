import { requestFailureMessage, type RequestFailure } from '../lib/errors';

/**
 * The filter and the picker of the models (#1011), one component for every screen that lists models or chooses one
 * (backend/internal/web/molecules/modelpick, modelcard).
 *
 * The script does NOT filter: a changed condition is a request to the server (htmx, the bar's `hx-get`), which answers the list that
 * modelfilter lets through. What it does:
 *
 * - The funnel (`[data-mp-toggle]`) opens and closes the panel of detailed filters (`[data-mp-panel]`); Esc closes it. When the server
 *   draws the panel again out of band (the answer to a chip's ×), it is as open or closed as it was.
 * - After the results of a list were swapped (`[data-model-results]`) the page gets a bubbling `model-pick-applied` event, for the
 *   code that reads the ticked models (the generation test's controls follow the models that are ticked).
 * - The picker: the button of a field (`[data-mp-open]`) asks for the picker, whose body arrives in the one dialog of the page
 *   (`dialog[data-model-pick-dialog]`) and is opened with showModal(). A card of the picker (`[data-mp-choose]`) puts the model it
 *   stands for into the hidden inputs of the field (`[data-mp-from="<key>"]`, the values in the card's `data-mp-values`), shows the
 *   card in the field, closes the dialog and sends `change` from the first input, so that the form that holds the field follows
 *   (a form that is drawn again for the model). The card of the model that is in the field now is marked (aria-pressed).
 * - Closing the dialog empties it and gives the focus back to the button that opened it.
 */

const DIALOG = 'dialog[data-model-pick-dialog]';
const BODY = '#mam-model-picker-body';
const FAILURE = '#mam-model-picker-failure';
const FIELD = '[data-model-pick-field]';

interface SwapDetail {
  requestConfig?: { elt?: Element };
}

interface OobDetail {
  target?: Element;
  fragment?: Element | DocumentFragment;
}

function dialog(): HTMLDialogElement | null {
  return document.querySelector<HTMLDialogElement>(DIALOG);
}

const targetOf = (event: Event) => event.target as Element | null;

/** The inputs a card sets in the field: key → input. */
function fieldInputs(field: Element): Map<string, HTMLInputElement> {
  const inputs = new Map<string, HTMLInputElement>();
  for (const input of field.querySelectorAll<HTMLInputElement>('[data-mp-from]'))
    inputs.set(input.dataset.mpFrom ?? 'value', input);
  return inputs;
}

function valuesOf(card: Element): Record<string, string> {
  try {
    return JSON.parse((card as HTMLElement).dataset.mpValues ?? '{}') as Record<string, string>;
  } catch {
    return {};
  }
}

/** Marks the card of the model that is in the field now, and only it. */
export function markCurrent(body: ParentNode, field: Element | null): void {
  const inputs = field ? fieldInputs(field) : new Map<string, HTMLInputElement>();
  for (const card of body.querySelectorAll<HTMLElement>('[data-mp-choose]')) {
    const values = Object.entries(valuesOf(card));
    const current = values.length > 0 && values.every(([key, value]) => inputs.get(key)?.value === value);
    card.setAttribute('aria-pressed', current ? 'true' : 'false');
    // The card of the model in the field looks chosen, as a card that is ticked does (the classes are written out so that Tailwind finds them).
    card.classList.toggle('border-accent', current);
    card.classList.toggle('bg-accent-soft', current);
    card.classList.toggle('border-border', !current);
    card.classList.toggle('bg-surface', !current);
  }
}

/** Puts the model of a card into its field. */
export function choose(card: HTMLElement, field: HTMLElement): void {
  const inputs = fieldInputs(field);
  const values = valuesOf(card);
  let first: HTMLInputElement | undefined;
  for (const [key, input] of inputs) {
    input.value = values[key] ?? '';
    first ??= input;
  }
  const current = field.querySelector('[data-mp-current]');
  if (current) current.replaceChildren(...[...card.childNodes].map((node) => node.cloneNode(true)));
  first?.dispatchEvent(new Event('change', { bubbles: true }));
}

/** Installs the filter and the picker. Call it once (main.ts). */
export function installModelPick(root: Document = document): void {
  let field: HTMLElement | null = null;
  let opener: HTMLElement | null = null;

  root.addEventListener('click', (event) => {
    const el = targetOf(event);
    const toggle = el?.closest<HTMLElement>('[data-mp-toggle]');
    if (toggle) {
      const panel = toggle.closest('[data-model-pick-bar]')?.querySelector<HTMLElement>('[data-mp-panel]');
      if (!panel) return;
      panel.hidden = !panel.hidden;
      toggle.setAttribute('aria-expanded', String(!panel.hidden));
      return;
    }
    const card = el?.closest<HTMLButtonElement>('[data-mp-choose]');
    const dlg = dialog();
    if (card && !card.disabled && field && dlg?.contains(card)) {
      choose(card, field);
      dlg.close();
      return;
    }
    if (el?.closest('[data-mp-close]')) dialog()?.close();
  });

  // Esc closes the panel that is open (the one the focus is in); the dialog's own Esc is left to it.
  root.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    const bar = targetOf(event)?.closest('[data-model-pick-bar]');
    const panel = bar?.querySelector<HTMLElement>('[data-mp-panel]');
    if (!bar || !panel || panel.hidden) return;
    panel.hidden = true;
    bar.querySelector('[data-mp-toggle]')?.setAttribute('aria-expanded', 'false');
    bar.querySelector<HTMLElement>('[data-mp-toggle]')?.focus();
    event.stopPropagation();
    event.preventDefault();
  });

  // The panel that comes out of band is as open as the one it replaces.
  root.addEventListener('htmx:oobBeforeSwap', (event) => {
    const { target, fragment } = (event as CustomEvent<OobDetail>).detail;
    if (!target?.matches('[data-mp-panel]')) return;
    const next =
      fragment instanceof Element && fragment.matches('[data-mp-panel]')
        ? fragment
        : fragment?.querySelector<HTMLElement>('[data-mp-panel]');
    if (next) (next as HTMLElement).hidden = (target as HTMLElement).hidden;
  });

  root.addEventListener('htmx:beforeRequest', (event) => {
    const elt = targetOf(event);
    if (elt?.matches('[data-mp-open]')) {
      opener = elt as HTMLElement;
      field = elt.closest<HTMLElement>(FIELD);
      const failure = dialog()?.querySelector<HTMLElement>(FAILURE);
      if (failure) {
        failure.hidden = true;
        failure.textContent = '';
      }
    }
  });

  root.addEventListener('htmx:afterSwap', (event) => {
    const el = targetOf(event);
    if (!el) return;
    if (el.matches(BODY)) {
      const requester = (event as CustomEvent<SwapDetail>).detail.requestConfig?.elt;
      const dlg = dialog();
      if (!dlg || !requester?.matches('[data-mp-open]')) return;
      markCurrent(el, field);
      if (!dlg.open) dlg.showModal();
      return;
    }
    if (el.matches('[data-model-results]')) {
      if (dialog()?.contains(el)) markCurrent(el, field);
      el.dispatchEvent(new CustomEvent('model-pick-applied', { bubbles: true }));
    }
  });

  root.addEventListener('htmx:beforeHistorySave', () => {
    const dlg = dialog();
    if (dlg?.open) dlg.close();
  });

  root.addEventListener(
    'close',
    (event) => {
      const dlg = dialog();
      if (!dlg || event.target !== dlg) return;
      dlg.querySelector(BODY)?.replaceChildren();
      const failure = dlg.querySelector<HTMLElement>(FAILURE);
      if (failure) {
        failure.hidden = true;
        failure.textContent = '';
      }
      const back = opener;
      opener = null;
      field = null;
      if (back?.isConnected) back.focus({ preventScroll: true });
    },
    true,
  );

  for (const type of [
    'htmx:responseError',
    'htmx:sendError',
    'htmx:timeout',
  ] as const satisfies readonly RequestFailure[]) {
    root.addEventListener(type, (event) => {
      const dlg = dialog();
      const failure = dlg?.querySelector<HTMLElement>(FAILURE);
      const el = targetOf(event);
      if (!dlg || !failure || !el || !(el.matches('[data-mp-open]') || dlg.contains(el))) return;
      const xhr = (event as CustomEvent<{ xhr?: XMLHttpRequest }>).detail?.xhr;
      failure.textContent = requestFailureMessage(type, xhr?.status);
      failure.hidden = false;
      if (el.matches('[data-mp-open]') && !dlg.open) dlg.showModal();
    });
  }
}
