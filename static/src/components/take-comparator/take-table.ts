import { html, nothing } from 'lit';
import type { TakeScore } from './take-model';
import { describeTake, renderScore } from './take-cells';
import { BADGE, STATUS_CLASS } from './take-status';

/** A button of a row: the text of `atoms.LinkButton` (W3), with the focus ring. */
const ROW_BUTTON = 'text-xs text-mute underline hover:text-ink focus-visible:focus-ring';
/** A number of the table (W7): right-aligned, digits of one width. */
const NUMBER = 'px-2 py-2 text-right font-mono tabular-nums';

/** What the buttons of a row do: the component owns the request, its busy state and the confirmation. */
export interface TakeActions {
  busy: boolean;
  select(take: TakeScore): void;
  reject(take: TakeScore): void;
}

export function renderTakeTable(takes: TakeScore[], minAxis: number, actions: TakeActions) {
  return html`<table class="w-full text-left text-sm" data-scores>
    <thead class="text-xs text-mute">
      <tr>
        <th class="px-2 py-1 font-semibold">Take</th>
        <th class="px-2 py-1 text-right font-semibold">Product</th>
        <th class="px-2 py-1 text-right font-semibold">Acting</th>
        <th class="px-2 py-1 text-right font-semibold">Camera</th>
        <th class="px-2 py-1 text-right font-semibold">Continuity</th>
        <th class="px-2 py-1 font-semibold">Status</th>
        <th class="px-2 py-1 font-semibold"><span class="sr-only">操作</span></th>
      </tr>
    </thead>
    <tbody>
      ${takes.map((t) => renderRow(t, minAxis, actions))}
    </tbody>
  </table>`;
}

function renderRow(t: TakeScore, minAxis: number, actions: TakeActions) {
  return html`<tr class="border-t border-border ${t.status === 'rejected' ? 'text-mute' : ''}" data-take=${t.id}>
    <th scope="row" class="px-2 py-2 font-semibold">
      Take ${t.label}${t.adopted ? html` <span class="${BADGE} bg-success/20 font-normal text-success" data-adopted>使用中</span>` : nothing}
      <div class="text-xs font-normal text-mute">${describeTake(t)}</div>
    </th>
    <td class="${NUMBER}" data-score="product">${renderScore(t.scores?.product, minAxis)}</td>
    <td class="${NUMBER}" data-score="acting">${renderScore(t.scores?.acting, minAxis)}</td>
    <td class="${NUMBER}" data-score="camera">${renderScore(t.scores?.camera, minAxis)}</td>
    <td class="${NUMBER}" data-score="continuity">${renderScore(t.scores?.continuity, minAxis)}</td>
    <td class="px-2 py-2">
      <span class="${BADGE} ${STATUS_CLASS[t.status]}" data-status=${t.status}>${t.status_label}</span>
      ${t.verdict ? html`<span class="ml-1 text-xs text-mute">${t.verdict.toUpperCase()}</span>` : nothing}
    </td>
    <td class="px-2 py-2 text-xs">
      ${
        t.select_url || t.reject_url
          ? html`<span class="flex gap-3">
            ${
              t.select_url
                ? html`<button type="button" aria-label="選択: Take ${t.label}" class=${ROW_BUTTON} data-action="select" ?disabled=${actions.busy} @click=${() => actions.select(t)}>選択</button>`
                : nothing
            }
            ${
              t.reject_url
                ? html`<button type="button" aria-label="除外: Take ${t.label}" class=${ROW_BUTTON} data-action="reject" ?disabled=${actions.busy} @click=${() => actions.reject(t)}>除外</button>`
                : nothing
            }
          </span>`
          : nothing
      }
    </td>
  </tr>`;
}
