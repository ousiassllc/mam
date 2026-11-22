import { html, nothing } from 'lit';
import { BADGE, BADGE_TONE } from '../lib/ui/badge';
import { BUTTON, BUTTON_TONE, LINK_BUTTON } from '../lib/ui/button';
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
      data-ripple
      class="${BUTTON} ${BUTTON_TONE.primary}"
      ?disabled=${view.busy || view.readonly || !dirty}
      @click=${() => view.save()}
    >
      保存
    </button>
    <button
      type="button"
      data-action="revert"
      class="${LINK_BUTTON}"
      ?disabled=${view.busy || !dirty}
      @click=${() => view.revert()}
    >
      変更を戻す
    </button>
    <span class="text-xs text-mute" data-total>合計 <span class="font-mono tabular-nums">${timelineSeconds(clips).toFixed(1)}</span> 秒 / <span class="font-mono tabular-nums">${clips.length}</span> クリップ</span>
    ${dirty ? html`<span class="${BADGE} ${BADGE_TONE.info}" data-dirty>未保存の変更あり</span>` : nothing}
    ${view.readonly ? html`<span class="${BADGE} ${BADGE_TONE.warning}" data-readonly>この段階では編集できません</span>` : nothing}
  </div>`;
}
