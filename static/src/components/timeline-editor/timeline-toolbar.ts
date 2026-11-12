import { html, nothing } from 'lit';
import { isDirty, timelineSeconds, type EditClip } from '../lib/edit-model';

/** What the toolbar under the clips shows and does: the editor owns the save, the reload and their focus. */
export interface ToolbarView {
  clips: EditClip[];
  /** The clips of the saved version. */
  saved: EditClip[];
  busy: boolean;
  readonly: boolean;
  save(): void;
  revert(): void;
}

/** The shape of `atoms.Badge` (W6). */
const BADGE = 'inline-block rounded-full px-2 py-1 text-xs';

export function renderToolbar(view: ToolbarView) {
  const { clips } = view;
  const dirty = isDirty(view.saved, clips);
  return html`<div class="flex flex-wrap items-center gap-3">
    <button
      type="button"
      data-action="save"
      data-ripple
      class="min-h-8 rounded-md bg-accent px-3 py-1 text-sm font-semibold text-bone transition-colors hover:bg-accent/85 disabled:opacity-40 motion-reduce:transition-none focus-visible:focus-ring"
      ?disabled=${view.busy || view.readonly || !dirty}
      @click=${() => view.save()}
    >
      保存
    </button>
    <button
      type="button"
      data-action="revert"
      class="text-xs text-mute underline transition-colors hover:text-ink disabled:opacity-40 motion-reduce:transition-none focus-visible:focus-ring"
      ?disabled=${view.busy || !dirty}
      @click=${() => view.revert()}
    >
      変更を戻す
    </button>
    <span class="text-xs text-mute" data-total>合計 <span class="font-mono tabular-nums">${timelineSeconds(clips).toFixed(1)}</span> 秒 / <span class="font-mono tabular-nums">${clips.length}</span> クリップ</span>
    ${dirty ? html`<span class="${BADGE} bg-info/20 text-info" data-dirty>未保存の変更あり</span>` : nothing}
    ${view.readonly ? html`<span class="${BADGE} bg-warning/20 text-warning" data-readonly>この段階では編集できません</span>` : nothing}
  </div>`;
}
