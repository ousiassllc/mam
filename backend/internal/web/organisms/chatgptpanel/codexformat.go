package chatgptpanel

import (
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
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

// methodURL is the section showing the login method (the tabs and the polling of a pending login).
func methodURL(method string) string { return paths.ChatGPTSettings() + "?method=" + method }

func codexStore(store string) string {
	switch store {
	case "keyring":
		return "OS の資格情報ストア"
	case "auto":
		return "OS の資格情報ストア（使えないときは CODEX_HOME のファイル）"
	}
	return "OS の資格情報ストア（未起動のため未確認）"
}
