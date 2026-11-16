import { svg } from 'lit';

/**
 * The small icon of a quiet notice of a Lit component, drawn like atoms.Icon (Iconoir, copied from backend/internal/web/atoms/icon_status.templ) in the muted ink:
 * the state is told by the icon and the words, not by a colour of a state (spec/components/color-rules.md). The notices themselves have the classes of
 * settingsui.Banner / settingsui.Note: `NOTICE_BANNER` / `NOTICE_NOTE` below.
 */
export const NOTICE_BANNER = 'flex gap-3 rounded-md border border-border border-l-2 bg-surface p-3 text-sm';
export const NOTICE_NOTE = 'flex items-start gap-2 text-sm text-ink';

export type NoticeIcon = 'info' | 'warning';

/** The icon of a notice; `extra` is added to the classes (the banner lowers it by one step: "mt-1"). */
export function noticeIcon(name: NoticeIcon, extra = '') {
  const cls = `${extra} h-5 w-5 shrink-0 text-mute`.trim();
  return name === 'warning'
    ? svg`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true" focusable="false" class=${cls}>
        <path d="M20.0429 21H3.95705C2.41902 21 1.45658 19.3364 2.22324 18.0031L10.2662 4.01533C11.0352 2.67792 12.9648 2.67791 13.7338 4.01532L21.7768 18.0031C22.5434 19.3364 21.581 21 20.0429 21Z" stroke-linecap="round"></path>
        <path d="M12 9V13" stroke-linecap="round"></path>
        <path d="M12 17.01L12.01 16.9989" stroke-linecap="round" stroke-linejoin="round"></path>
      </svg>`
    : svg`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true" focusable="false" class=${cls}>
        <path d="M12 11.5V16.5" stroke-linecap="round" stroke-linejoin="round"></path>
        <path d="M12 7.51L12.01 7.49889" stroke-linecap="round" stroke-linejoin="round"></path>
        <path d="M12 22C17.5228 22 22 17.5228 22 12C22 6.47715 17.5228 2 12 2C6.47715 2 2 6.47715 2 12C2 17.5228 6.47715 22 12 22Z" stroke-linecap="round" stroke-linejoin="round"></path>
      </svg>`;
}
