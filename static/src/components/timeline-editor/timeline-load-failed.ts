import { html, nothing } from 'lit';

/**
 * What the editor shows when its first load failed (not a 404): the reason is the alert in #alert-region, so this says where to look and
 * offers "もう一度読み込む" (spec/components/lit-widgets-guide.md §3.2), so that the way on is not to open the page again.
 */
export function renderLoadFailed(retrying: boolean, retry: () => void) {
  return html`<div class="flex flex-col items-start gap-3 rounded-lg border border-dashed border-border p-6 text-sm text-mute" data-load-failed>
    <p>タイムラインを読み込めませんでした。理由は通知に出ています。</p>
    <button
      type="button"
      data-action="retry"
      data-ripple
      aria-busy=${retrying ? 'true' : 'false'}
      class="min-h-8 rounded-md border border-border px-3 py-1 text-sm text-ink transition-colors hover:bg-accent-soft motion-reduce:transition-none focus-visible:focus-ring"
      @click=${retry}
    >
      もう一度読み込む
    </button>
    ${retrying ? html`<p role="status">読み込み中…</p>` : nothing}
  </div>`;
}
