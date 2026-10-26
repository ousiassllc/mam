package toolstatus_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/toolstatus"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func one(tone atoms.BadgeTone, text string) []molecules.StatusLine {
	return []molecules.StatusLine{{Tone: tone, Text: text}}
}

// The table of spec/components/home-screen.md §5: what the node card and the model manager card say for each state of
// the bundled engine and each count of external ComfyUIs.
func TestComfyUI_LinesOfTheNodeAndModelManagerCards(t *testing.T) {
	bundled := func(state proc.State, external int) toolstatus.ComfyUI {
		return toolstatus.ComfyUI{Supported: true, Installed: true, State: state, External: external}
	}
	tests := []struct {
		name      string
		c         toolstatus.ComfyUI
		nodes, mm []molecules.StatusLine
	}{
		{"running", bundled(proc.StateRunning, 0), one(atoms.BadgeOK, "ComfyUI: 起動中"), one(atoms.BadgeOK, "ComfyUI: 起動中")},
		{"starting", bundled(proc.StateStarting, 0), one(atoms.BadgeInfo, "ComfyUI: 起動しています"), one(atoms.BadgeInfo, "ComfyUI: 起動しています")},
		{"stopped", bundled(proc.StateStopped, 0), one(atoms.BadgeNeutral, "ComfyUI: 停止中"), one(atoms.BadgeNeutral, "ComfyUI: 停止中")},
		{"failed", bundled(proc.StateFailed, 0), one(atoms.BadgeError, "ComfyUI: 起動に失敗"), one(atoms.BadgeError, "ComfyUI: 起動に失敗")},
		{"installed with externals shows the bundled one only", bundled(proc.StateRunning, 2), one(atoms.BadgeOK, "ComfyUI: 起動中"), one(atoms.BadgeOK, "ComfyUI: 起動中")},
		{
			"not installed, one external", toolstatus.ComfyUI{Supported: true, External: 1},
			one(atoms.BadgeNeutral, "外部の ComfyUI: 1 件"), one(atoms.BadgeNeutral, "外部の ComfyUI: 1 件"),
		},
		{
			"not installed, none", toolstatus.ComfyUI{Supported: true},
			one(atoms.BadgeWarn, "ComfyUI が必要です"), one(atoms.BadgeNeutral, "ComfyUI: 未導入"),
		},
		{
			"unsupported OS, externals", toolstatus.ComfyUI{External: 3},
			one(atoms.BadgeNeutral, "外部の ComfyUI: 3 件"), one(atoms.BadgeNeutral, "外部の ComfyUI: 3 件"),
		},
		{
			"unsupported OS, none", toolstatus.ComfyUI{},
			one(atoms.BadgeWarn, "ComfyUI が必要です"), one(atoms.BadgeNeutral, "同梱の ComfyUI は Windows 版のみ"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.nodes, tt.c.NodesLines())
			assert.Equal(t, tt.mm, tt.c.ModelManagerLines())
		})
	}
}

func TestCountLine(t *testing.T) {
	assert.Equal(t, one(atoms.BadgeNeutral, "保存 3 件"), toolstatus.CountLine("保存", 3, "保存なし"))
	assert.Equal(t, one(atoms.BadgeNeutral, "保存なし"), toolstatus.CountLine("保存", 0, "保存なし"), "no item is neutral too: nothing is wrong")
}

func TestSettingsLines(t *testing.T) {
	tests := []struct {
		name       string
		configured int
		keysErr    error
		login      *toolstatus.Login
		want       []molecules.StatusLine
	}{
		{"keys and signed in", 3, nil, &toolstatus.Login{SignedIn: true}, []molecules.StatusLine{
			{Tone: atoms.BadgeInfo, Text: "API キー 3 / 10 件"}, {Tone: atoms.BadgeOK, Text: "ChatGPT: ログイン済み"}}},
		{"no key is a warning", 0, nil, &toolstatus.Login{}, []molecules.StatusLine{
			{Tone: atoms.BadgeWarn, Text: "API キー 0 / 10 件"}, {Tone: atoms.BadgeNeutral, Text: "ChatGPT: 未ログイン"}}},
		{"relogin wins over signed in", 1, nil, &toolstatus.Login{SignedIn: true, NeedsRelogin: true}, []molecules.StatusLine{
			{Tone: atoms.BadgeInfo, Text: "API キー 1 / 10 件"}, {Tone: atoms.BadgeWarn, Text: "ChatGPT: 再ログインが必要"}}},
		{"key store unreadable", 0, errors.New("keyring down"), nil, one(atoms.BadgeWarn, "キーストアを利用できません")},
		{"no ChatGPT service: the key line only", 2, nil, nil, one(atoms.BadgeInfo, "API キー 2 / 10 件")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toolstatus.SettingsLines(tt.configured, 10, tt.keysErr, tt.login))
		})
	}
}
