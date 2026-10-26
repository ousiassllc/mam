package toolstatus

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// ComfyUI is what the node and model manager cards know of ComfyUI: the bundled engine and how many external
// ComfyUIs are registered. An external one is only counted: reaching it can take 10 seconds and each screen checks
// it itself.
type ComfyUI struct {
	// Supported: the bundled engine exists on this OS (Windows). Installed: it is installed and its files are there.
	Supported, Installed bool
	State                proc.State
	External             int
}

func line(tone atoms.BadgeTone, text string) []molecules.StatusLine {
	return []molecules.StatusLine{{Tone: tone, Text: text}}
}

// bundled is the line of the bundled engine, "" when it is not installed (or cannot be here).
func (c ComfyUI) bundled() []molecules.StatusLine {
	if !c.Supported || !c.Installed {
		return nil
	}
	switch c.State {
	case proc.StateRunning:
		return line(atoms.BadgeOK, "ComfyUI: 起動中")
	case proc.StateStarting:
		return line(atoms.BadgeInfo, "ComfyUI: 起動しています")
	case proc.StateFailed:
		return line(atoms.BadgeError, "ComfyUI: 起動に失敗")
	}
	return line(atoms.BadgeNeutral, "ComfyUI: 停止中")
}

func (c ComfyUI) external() []molecules.StatusLine {
	if c.External == 0 {
		return nil
	}
	return line(atoms.BadgeNeutral, fmt.Sprintf("外部の ComfyUI: %d 件", c.External))
}

// NodesLines is the status of the node card: the screen needs a ComfyUI, so with none it says so.
func (c ComfyUI) NodesLines() []molecules.StatusLine {
	if l := c.bundled(); l != nil {
		return l
	}
	if l := c.external(); l != nil {
		return l
	}
	return line(atoms.BadgeWarn, "ComfyUI が必要です")
}

// ModelManagerLines is the status of the model manager card: the screen is where ComfyUI is installed, so with none it
// only says what is the case.
func (c ComfyUI) ModelManagerLines() []molecules.StatusLine {
	if l := c.bundled(); l != nil {
		return l
	}
	if l := c.external(); l != nil {
		return l
	}
	if !c.Supported {
		return line(atoms.BadgeNeutral, "同梱の ComfyUI は Windows 版のみ")
	}
	return line(atoms.BadgeNeutral, "ComfyUI: 未導入")
}

// CountLine is a neutral line with a count: "<what> n 件", or none ("保存なし") for 0.
func CountLine(what string, n int, none string) []molecules.StatusLine {
	if n == 0 {
		return line(atoms.BadgeNeutral, none)
	}
	return line(atoms.BadgeNeutral, fmt.Sprintf("%s %d 件", what, n))
}

// Login is the ChatGPT sign-in as the settings card shows it, over the three methods (official, image generation
// (unofficial), Codex).
type Login struct {
	// SignedIn: one of the methods is signed in. NeedsRelogin: one of them has to be signed in again.
	SignedIn, NeedsRelogin bool
}

// SettingsLines is the status of the settings card: how many API keys are set and, when the ChatGPT sign-in can be
// read, its state. keysErr is the key store that cannot be read.
func SettingsLines(configured, total int, keysErr error, login *Login) []molecules.StatusLine {
	var out []molecules.StatusLine
	switch {
	case keysErr != nil:
		out = line(atoms.BadgeWarn, "キーストアを利用できません")
	case configured == 0:
		out = line(atoms.BadgeWarn, fmt.Sprintf("API キー %d / %d 件", configured, total))
	default:
		out = line(atoms.BadgeInfo, fmt.Sprintf("API キー %d / %d 件", configured, total))
	}
	if login == nil {
		return out
	}
	switch {
	case login.NeedsRelogin:
		return append(out, molecules.StatusLine{Tone: atoms.BadgeWarn, Text: "ChatGPT: 再ログインが必要"})
	case login.SignedIn:
		return append(out, molecules.StatusLine{Tone: atoms.BadgeOK, Text: "ChatGPT: ログイン済み"})
	}
	return append(out, molecules.StatusLine{Tone: atoms.BadgeNeutral, Text: "ChatGPT: 未ログイン"})
}
