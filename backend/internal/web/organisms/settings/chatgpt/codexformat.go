package chatgpt

import (
	"fmt"
	"time"
)

// jst is Japan Standard Time, the zone the reset times of the Codex usage limit are shown in (it has no daylight saving time).
var jst = time.FixedZone("JST", 9*60*60)

// formatJST is a time as the user reads it: "2026-10-25 18:30（日本時間）".
func formatJST(t time.Time) string {
	return t.In(jst).Format("2006-01-02 15:04") + "（日本時間）"
}

// engineLabel and engineTone are the badge of the engine state.
func engineLabel(state string) string {
	switch state {
	case "ready":
		return "準備済み"
	case "running":
		return "起動中"
	case "installing":
		return "取得中"
	case "failed":
		return "異常"
	}
	return "未取得"
}

// megabytes is a size in MB (10^6 bytes) rounded to the nearest, as the download notice shows it.
func megabytes(n int64) int { return int((n + 500_000) / 1_000_000) }

// downloadNotice is the sizes of the pinned asset for the notice before the download ("" when they are not known):
// the archive differs by platform (a zip on Windows, a tar.gz on macOS), and so does its size.
func downloadNotice(e CodexEngine) string {
	if e.DownloadBytes <= 0 || e.ExtractedBytes <= 0 {
		return ""
	}
	return fmt.Sprintf("約 %d MB。展開後は約 %d MB。", megabytes(e.DownloadBytes), megabytes(e.ExtractedBytes))
}

func engineSource(source string) string {
	switch source {
	case "download":
		return "公式のリリースから取得したもの"
	case "custom":
		return "指定したパスの codex"
	}
	return ""
}

func limitText(l CodexLimit) string {
	name := l.Name
	if name == "" {
		name = "利用上限"
	}
	text := fmt.Sprintf("%s: %d%% 使用", name, l.UsedPercent)
	if !l.ResetsAt.IsZero() {
		text += "（リセット " + formatJST(l.ResetsAt) + "）"
	}
	return text
}

func percentText(n int) string { return fmt.Sprintf("%d", n) }

func codexStore(store string) string {
	switch store {
	case "keyring":
		return "OS の資格情報ストア"
	case "auto":
		return "OS の資格情報ストア（使えないときは CODEX_HOME のファイル）"
	}
	return "OS の資格情報ストア（未起動のため未確認）"
}
