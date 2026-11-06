// Package mainline describes why the main line of a Project (Brief → script → Scene / Shot → generation) stands still after the
// Scene / Shot plan (#1108).
package mainline

import (
	"fmt"
	"strings"
)

// Wait is what the main line waits for. The Orchestrator plans up to the Scenes and Shots and never starts a generation by itself
// (a generation costs money, so the user starts it per Shot, spec/requirements/functional.md UC-1). The Confirmation Level only
// decides which Approval cards are posted (script, Rough Cut, Final); it does not hold a generation back. Saying so, and what to
// press next, is what keeps a Shot that stays PLANNING from looking stuck.
type Wait struct {
	// Shots is the number of Shots that are PLANNING and have no Take.
	Shots int
	// Counted is whether the Library and the Characters were read; when false the wait text leaves them out.
	Counted bool
	// References is the number of items of the Reference Library of the Project.
	References int
	// Characters is the number of Characters of its World State.
	Characters int
}

// Active reports whether there is a Shot the main line waits on.
func (w Wait) Active() bool { return w.Shots > 0 }

// Gaps lists what the Shots would be made without: nothing blocks a generation, but a Shot made without them has no fixed face or look.
func (w Wait) Gaps() []string {
	if !w.Counted {
		return nil
	}
	var gaps []string
	if w.References == 0 {
		gaps = append(gaps, "Reference Library が 0 件")
	}
	if w.Characters == 0 {
		gaps = append(gaps, "登場人物（World State の Character）が 0 人")
	}
	return gaps
}

// Reason is the sentence that says what the Project waits for.
func (w Wait) Reason() string {
	text := fmt.Sprintf("Shot %d 件が PLANNING（Take なし）のまま、生成の開始を待っています。生成は有料なので自動では始まりません。確認レベルは脚本・Rough Cut・Final の承認カードだけを決め、生成の開始は止めていません。", w.Shots)
	if gaps := w.Gaps(); len(gaps) > 0 {
		text += "現在は " + strings.Join(gaps, "、") + "です。生成はできますが、顔や見た目の一貫性を保つ参照がありません。"
	}
	return text
}

// NextAction is what to press next.
func (w Wait) NextAction() string {
	return "次に押すもの: Scene / Shot で Shot を選び、必要なら先に「Reference Pack」、続けて「生成」を押してください。起動した生成は Job と Progress で追えます。"
}

// Text is Reason and NextAction in one message.
func (w Wait) Text() string { return w.Reason() + "\n" + w.NextAction() }
