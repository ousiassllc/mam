import { html } from 'lit';
import { BUTTON, BUTTON_TONE, LINK_BUTTON } from '../lib/ui/button';
import { icon } from '../lib/ui/icon';
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
  return html`<div class="flex flex-col gap-2 rounded-lg border border-border bg-surface p-3" tabindex="-1" data-selection>
    ${
      selection.length === 0
        ? html`<p class="text-xs text-mute">Take の帯をドラッグして区間を選ぶと、選んだ順につなぎます。</p>`
        : html`<ol class="flex flex-wrap gap-2 text-sm">
            ${selection.map(
              (s, i) => html`<li class="flex items-center gap-1 rounded-sm bg-accent-soft px-2 py-1" data-segment>
                <span>${describeSegments([s], view.clips)}</span>
                <button type="button" class="inline-flex min-h-6 min-w-6 items-center justify-center rounded-md text-mute transition-colors hover:text-ink focus-visible:focus-ring motion-reduce:transition-none" aria-label="区間 ${i + 1} を外す" data-action="remove-segment" data-ripple @click=${() => view.removeSegment(i)}>${icon('close')}</button>
              </li>`,
            )}
          </ol>
          <p class="text-xs text-mute">合計 <span class="font-mono tabular-nums">${totalSeconds(selection).toFixed(1)}</span> 秒（つなぎ目はクロスフェード）</p>`
    }
    <div class="flex flex-wrap items-center gap-2">
      <button type="button" data-action="confirm-partial" class="${BUTTON} ${BUTTON_TONE.primary}" data-ripple ?disabled=${!view.canMerge} @click=${() => view.merge()}>
        この区間で合成
      </button>
      <button type="button" data-action="clear-selection" class="${LINK_BUTTON}" ?disabled=${selection.length === 0} @click=${() => view.clear()}>選び直す</button>
    </div>
    <form class="flex flex-wrap items-center gap-2" @submit=${(e: Event) => view.submitInstruction(e)}>
      <input
        type="text"
        name="instruction"
        class="min-w-64 flex-1 rounded-md border border-mute bg-surface px-2 py-1 text-sm text-ink focus-visible:focus-ring"
        placeholder="Take A の 0-2秒 + Take B の 2-4秒"
        aria-label="区間を言葉で指定"
        .value=${view.instruction}
        @input=${(e: Event) => view.setInstruction((e.target as HTMLInputElement).value)}
      />
      <button type="submit" data-action="confirm-instruction" class="${BUTTON} ${BUTTON_TONE.neutral}" data-ripple ?disabled=${view.busy || view.instruction.trim() === ''}>
        言葉で合成
      </button>
    </form>
  </div>`;
}
