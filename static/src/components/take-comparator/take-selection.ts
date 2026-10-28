import { html } from 'lit';
import { describeSegments, totalSeconds, type Clip, type Segment } from '../lib/timeline-model';

/** What the picked parts show and do: the component owns the selection, the instruction and the requests. */
export interface SelectionView {
  selection: Segment[];
  clips: Clip[];
  instruction: string;
  busy: boolean;
  canMerge: boolean;
  removeSegment(index: number): void;
  clear(): void;
  merge(): void;
  setInstruction(text: string): void;
  submitInstruction(event: Event): void;
}

/** The picked parts, the buttons that join them and the box to say it in words. `tabindex="-1"`: where the focus goes when the button it was on is gone. */
export function renderSelection(view: SelectionView) {
  const { selection } = view;
  return html`<div class="flex flex-col gap-2 rounded border border-neutral-700 p-3" tabindex="-1" data-selection>
    ${
      selection.length === 0
        ? html`<p class="text-xs text-neutral-400">Take の帯をドラッグして区間を選ぶと、選んだ順につなぎます。</p>`
        : html`<ol class="flex flex-wrap gap-2 text-xs">
            ${selection.map(
              (s, i) => html`<li class="flex items-center gap-1 rounded bg-neutral-800 px-2 py-1" data-segment>
                <span>${describeSegments([s], view.clips)}</span>
                <button type="button" class="text-neutral-400 hover:text-neutral-100" aria-label="区間 ${i + 1} を外す" data-action="remove-segment" @click=${() => view.removeSegment(i)}>×</button>
              </li>`,
            )}
          </ol>
          <p class="text-xs text-neutral-400">合計 ${totalSeconds(selection).toFixed(1)} 秒（つなぎ目はクロスフェード）</p>`
    }
    <div class="flex flex-wrap items-center gap-2">
      <button type="button" data-action="confirm-partial" class="rounded bg-indigo-500 px-3 py-1 text-xs font-medium text-white hover:bg-indigo-400 disabled:opacity-40" ?disabled=${!view.canMerge} @click=${() => view.merge()}>
        この区間で合成
      </button>
      <button type="button" data-action="clear-selection" class="text-xs text-neutral-400 underline" ?disabled=${selection.length === 0} @click=${() => view.clear()}>選び直す</button>
    </div>
    <form class="flex flex-wrap items-center gap-2" @submit=${(e: Event) => view.submitInstruction(e)}>
      <input
        type="text"
        name="instruction"
        class="min-w-64 flex-1 rounded border border-neutral-500 bg-neutral-900 px-2 py-1 text-xs"
        placeholder="Take A の 0-2秒 + Take B の 2-4秒"
        aria-label="区間を言葉で指定"
        .value=${view.instruction}
        @input=${(e: Event) => view.setInstruction((e.target as HTMLInputElement).value)}
      />
      <button type="submit" data-action="confirm-instruction" class="rounded border border-neutral-600 px-3 py-1 text-xs disabled:opacity-40" ?disabled=${view.busy || view.instruction.trim() === ''}>
        言葉で合成
      </button>
    </form>
  </div>`;
}
