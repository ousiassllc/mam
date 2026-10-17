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

export function renderToolbar(view: ToolbarView) {
  const { clips } = view;
  const dirty = isDirty(view.saved, clips);
  return html`<div class="flex flex-wrap items-center gap-3">
    <button
      type="button"
      data-action="save"
      class="rounded bg-neutral-200 px-3 py-1 text-xs font-medium text-neutral-900 disabled:opacity-40"
      ?disabled=${view.busy || view.readonly || !dirty}
      @click=${() => view.save()}
    >
      保存
    </button>
    <button type="button" data-action="revert" class="text-xs text-neutral-400 underline disabled:opacity-40" ?disabled=${view.busy || !dirty} @click=${() => view.revert()}>
      変更を戻す
    </button>
    <span class="text-xs text-neutral-400" data-total>合計 ${timelineSeconds(clips).toFixed(1)} 秒 / ${clips.length} クリップ${dirty ? '（未保存の変更あり）' : ''}</span>
    ${view.readonly ? html`<span class="text-xs text-amber-300" data-readonly>この段階では編集できません</span>` : nothing}
  </div>`;
}
