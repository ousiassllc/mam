// Package mainline describes why the main line of a Project (Brief → script → Scene / Shot → generation) stands still after the
// Scene / Shot plan (#1108) and what the user is told to do about it.
package mainline

import (
	"fmt"
	"strings"
)

// Phase is what the main line is waiting for after the Scene / Shot plan.
type Phase string

// Phases. In PhaseManual nothing starts the generation but the user (DIRECTOR / MANUAL mode, or no generation wired); in the others the
// Orchestrator carries the Project on by itself once the generation approval is given (spec/spec.md §4.3).
const (
	PhaseManual   Phase = ""
	PhaseApproval Phase = "approval" // the approval card is waiting for an answer (or is about to be posted)
	PhaseDeclined Phase = "declined" // the approval was declined: nothing is made until the plan changes or the user asks again
	PhaseAuto     Phase = "auto"     // approved: the Orchestrator starts the Reference Packs and the Shots
)

// DeclinedAck is what the chat says when the generation approval is declined: nothing is made, and what to do next.
const DeclinedAck = "差し戻しを受け付けました。生成は始めません（承認されるまで、Reference Pack・Keyframe・Video は一切始まりません）。" +
	"次の行動: ①計画を直す（Shot の追加、動画モデルの固定、Quality Policy の変更）、②範囲を狭める（Shot を減らす）。範囲や見積が変わると、改めて承認を求めます。" +
	"③そのままもう一度承認を求めるには、チャットに何かメッセージを送ってください。"

// Wait is what the main line waits for. A Shot that stays PLANNING with no Take is an intended wait, and this says why and what the user does.
type Wait struct {
	// Phase is what the wait is for.
	Phase Phase
	// Shots is the number of Shots that are PLANNING and have no Take.
	Shots int
	// Stuck is the number of those Shots the Orchestrator cannot carry on by itself (a Reference Pack that failed, a budget ceiling, a start that
	// failed): the user starts them by hand. It is counted in PhaseAuto only.
	Stuck int
	// Counted is whether the Library and the Characters were read; when false the wait text leaves them out.
	Counted bool
	// References is the number of items of the Reference Library of the Project.
	References int
	// Characters is the number of Characters of its World State.
	Characters int
}

// Active reports whether there is a Shot the main line waits on.
func (w Wait) Active() bool { return w.Shots > 0 }

// Title is the heading of the notice on the Scene / Shot screen.
func (w Wait) Title() string {
	switch w.Phase {
	case PhaseApproval:
		return "本線は生成の承認を待っています"
	case PhaseDeclined:
		return "生成の承認は差し戻されました。生成は始まりません"
	case PhaseAuto:
		return "承認済みです。本線が自動で生成を進めています"
	}
	return "本線は Shot の生成開始を待っています"
}

// ShotLabel is the badge of a Shot that is PLANNING with no Take.
func (w Wait) ShotLabel() string {
	switch w.Phase {
	case PhaseApproval, PhaseDeclined:
		return "承認待ち"
	case PhaseAuto:
		return "自動開始待ち"
	}
	return "生成待ち"
}

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
	var text string
	switch w.Phase {
	case PhaseApproval:
		text = fmt.Sprintf("Shot %d 件が PLANNING（Take なし）のまま、生成の承認を待っています。生成は有料なので、承認するまで Reference Pack・Keyframe・Video は一切始まりません。"+
			"承認すると、Reference Pack → Shot 生成が自動で進みます。確認レベルが FULL AUTO でも、この承認は必ず求めます。", w.Shots)
	case PhaseDeclined:
		text = fmt.Sprintf("生成の承認が差し戻されたため、Shot %d 件は PLANNING（Take なし）のままで、生成は始まりません。", w.Shots)
	case PhaseAuto:
		text = fmt.Sprintf("生成を承認済みです。まだ始まっていない Shot %d 件の Reference Pack → 生成を、Orchestrator が自動で進めています。", w.Shots)
		if w.Stuck > 0 {
			text += fmt.Sprintf("うち %d 件は自動では進められません（Reference Pack の失敗・予算上限・起動の失敗。理由はチャットにあります）。", w.Stuck)
		}
	default:
		text = fmt.Sprintf("Shot %d 件が PLANNING（Take なし）のまま、生成の開始を待っています。生成は有料なので自動では始まりません。"+
			"DIRECTOR / MANUAL（または生成の仕組みが未設定）では「生成の承認」は出さず、Shot ごとに起動します。", w.Shots)
	}
	if gaps := w.Gaps(); len(gaps) > 0 {
		text += "現在は " + strings.Join(gaps, "、") + "です。生成はできますが、顔や見た目の一貫性を保つ参照がありません。"
	}
	return text
}

// NextAction is what to do next.
func (w Wait) NextAction() string {
	switch w.Phase {
	case PhaseApproval:
		return "次にするもの: チャットの「生成の承認」カードで、内容と費用の見積を確かめて、承認か修正の依頼（差戻し）を選んでください。"
	case PhaseDeclined:
		return "次にするもの: 計画を直す（Shot の追加、動画モデルの固定、Quality Policy の変更）か Shot を減らすと、改めて承認を求めます。そのままもう一度求めるには、チャットに何かメッセージを送ってください。"
	case PhaseAuto:
		if w.Stuck > 0 {
			return "次にするもの: 進められない Shot は、Shot を選んで「Reference Pack」「生成」を押してください。ほかは何も要りません。起動した生成は Job と Progress で追えます。"
		}
		return "次にするもの: ありません。起動した生成は Job と Progress で追えます。"
	}
	return "次に押すもの: Scene / Shot で Shot を選び、必要なら先に「Reference Pack」、続けて「生成」を押してください。起動した生成は Job と Progress で追えます。"
}

// Text is Reason and NextAction in one message.
func (w Wait) Text() string { return w.Reason() + "\n" + w.NextAction() }
