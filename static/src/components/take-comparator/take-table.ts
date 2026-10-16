import { html, nothing } from 'lit';
import type { TakeScore } from './take-model';
import { describeTake, renderScore } from './take-cells';
import { STATUS_CLASS } from './take-status';

/** What the buttons of a row do: the component owns the request, its busy state and the confirmation. */
export interface TakeActions {
  busy: boolean;
  select(url: string): void;
  reject(url: string): void;
}

export function renderTakeTable(takes: TakeScore[], minAxis: number, actions: TakeActions) {
  return html`<table class="w-full text-left text-sm" data-scores>
    <thead class="text-xs text-neutral-400">
      <tr>
        <th class="px-2 py-1 font-medium">Take</th>
        <th class="px-2 py-1 font-medium">Product</th>
        <th class="px-2 py-1 font-medium">Acting</th>
        <th class="px-2 py-1 font-medium">Camera</th>
        <th class="px-2 py-1 font-medium">Continuity</th>
        <th class="px-2 py-1 font-medium">Status</th>
        <th class="px-2 py-1 font-medium"><span class="sr-only">操作</span></th>
      </tr>
    </thead>
    <tbody>
      ${takes.map((t) => renderRow(t, minAxis, actions))}
    </tbody>
  </table>`;
}

function renderRow(t: TakeScore, minAxis: number, actions: TakeActions) {
  return html`<tr class="border-t border-neutral-800 ${t.status === 'rejected' ? 'text-neutral-400' : ''}" data-take=${t.id}>
    <th scope="row" class="px-2 py-2 font-medium">
      Take ${t.label}${t.adopted ? html` <span class="text-xs text-emerald-300" data-adopted>（使用中）</span>` : nothing}
      <div class="text-xs font-normal text-neutral-400">${describeTake(t)}</div>
    </th>
    <td class="px-2 py-2" data-score="product">${renderScore(t.scores?.product, minAxis)}</td>
    <td class="px-2 py-2" data-score="acting">${renderScore(t.scores?.acting, minAxis)}</td>
    <td class="px-2 py-2" data-score="camera">${renderScore(t.scores?.camera, minAxis)}</td>
    <td class="px-2 py-2" data-score="continuity">${renderScore(t.scores?.continuity, minAxis)}</td>
    <td class="px-2 py-2">
      <span class="rounded border px-2 py-0.5 text-xs ${STATUS_CLASS[t.status]}" data-status=${t.status}>${t.status_label}</span>
      ${t.verdict ? html`<span class="ml-1 text-xs text-neutral-400">${t.verdict.toUpperCase()}</span>` : nothing}
    </td>
    <td class="px-2 py-2 text-xs">
      ${
        t.select_url || t.reject_url
          ? html`<span class="flex gap-3">
            ${
              t.select_url
                ? html`<button type="button" aria-label="選択: Take ${t.label}" class="underline hover:text-neutral-100" data-action="select" ?disabled=${actions.busy} @click=${() => actions.select(t.select_url!)}>選択</button>`
                : nothing
            }
            ${
              t.reject_url
                ? html`<button type="button" aria-label="除外: Take ${t.label}" class="underline hover:text-neutral-100" data-action="reject" ?disabled=${actions.busy} @click=${() => actions.reject(t.reject_url!)}>除外</button>`
                : nothing
            }
          </span>`
          : nothing
      }
    </td>
  </tr>`;
}
