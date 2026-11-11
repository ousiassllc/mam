// Package mainline describes why the main line of a Project (Brief → script → Scene / Shot → generation) stands still after the
// Scene / Shot plan (#1108) and what the user is told to do about it.
package mainline

import (
	"fmt"
	"strings"
)

// Phase is what the main line is waiting for after the Scene / Shot plan.
type Phase string

// Phases. In PhaseManual nothing starts the generation but the user (DIRECTOR / MANUAL mode, or no generation wired); in PhaseAuto the Orchestrator
// starts the Reference Packs and the Shots by itself, without asking: choosing AUTO is the consent (spec/spec.md §4.3).
const (
	PhaseManual   Phase = ""
	PhaseDeclined Phase = "declined" // a generation approval card of v0.1.34 was declined: nothing starts by itself until the user writes or the plan changes
	// PhaseDeclinedUnrecorded is the same for a card that has no record of what it showed (it came with an imported bundle, or its record was lost): the plan
	// cannot be compared with the card, so only a message of the user lifts the decline.
	PhaseDeclinedUnrecorded Phase = "declined_unrecorded"
	PhaseAuto               Phase = "auto"           // the Orchestrator starts the Reference Packs and the Shots
	PhaseBudgetConfirm      Phase = "budget_confirm" // PhaseAuto held until the user answers the confirmation of the budget they stated in the chat (#1139): the ceiling is not set yet
)

// DeclinedAck is what the chat says when a generation approval card of v0.1.34 is declined in an AUTO Project (an old card; no new one is posted):
// nothing starts by itself for now, and what to do next.
const DeclinedAck = "差し戻しを受け付けました。この「生成の承認」は旧方針のカードで、いまは AUTO では承認を求めません。差し戻されたので、自動では生成を始めません。" +
	"次の行動: ①自動で始めるには、チャットに何かメッセージを送ってください。②計画（Shot の追加、動画モデルの固定、Quality Policy の変更）を直すと、直した計画で自動で始めます。" +
	"③自分で始めるなら、Shot を選んで手動で起動してください。"

// UnrecordedCardNote is what a generation approval card of v0.1.34 that has no record says on the card: it is never an approval, and what starts the
// generation in an AUTO Project is AUTO, not the card. It is the same for a card that is pending, approved or declined.
const UnrecordedCardNote = "このカードには記録が無い（取り込んだ Project など）ため、承認としては扱いません。AUTO の Project が生成を始めるのは AUTO を選んでいるためで、このカードのためではありません。"

// DeclinedAckUnrecorded is DeclinedAck for a card that has no record: changing the plan does not lift the decline, only a message does.
const DeclinedAckUnrecorded = "差し戻しを受け付けました。この「生成の承認」は旧方針のカードで、いまは AUTO では承認を求めません。このカードには記録が無い（取り込んだ Project など）ため、" +
	"計画を直しても差し戻しは解けません。差し戻されたので、自動では生成を始めません。" +
	"次の行動: ①自動で始めるには、チャットに何かメッセージを送ってください。②自分で始めるなら、Shot を選んで手動で起動してください。"

// DeclinedAckNotAuto is what the chat says when the card is declined in a Project that is not AUTO: nothing starts by itself there anyway, and the decline
// stays. After a switch to AUTO a message of the user lifts it; for a card with a record a change of the plan does too, for one without (unrecorded) it
// does not.
func DeclinedAckNotAuto(unrecorded bool) string {
	lifts := "チャットへメッセージを送るか、計画（Shot の追加、動画モデルの固定、Quality Policy の変更）を直す"
	if unrecorded {
		lifts = "チャットへメッセージを送る（このカードには記録が無いため、計画を直しても解けません）"
	}
	return "差し戻しを受け付けました。この「生成の承認」は旧方針のカードです。この Project は AUTO ではないため、生成はもともと自動では始まりません。" +
		"Shot を選んで手動で起動してください。差し戻しは残るので、あとで AUTO にしても、このあとに" + lifts + "までは自動で始まりません。"
}

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
	case PhaseDeclined, PhaseDeclinedUnrecorded:
		return "旧方針の生成の承認が差し戻されています。自動では始めません"
	case PhaseAuto:
		return "AUTO のため、本線が自動で生成を進めます"
	case PhaseBudgetConfirm:
		return "予算の確認を待っています。確認するまで生成は始めません"
	}
	return "本線は Shot の生成開始を待っています"
}

// ShotLabel is the badge of a Shot that is PLANNING with no Take.
func (w Wait) ShotLabel() string {
	switch w.Phase {
	case PhaseDeclined, PhaseDeclinedUnrecorded:
		return "自動開始を保留"
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
	case PhaseDeclined, PhaseDeclinedUnrecorded:
		text = fmt.Sprintf("旧方針で出た「生成の承認」カードが差し戻されているため、Shot %d 件は PLANNING（Take なし）のままで、自動では始めません。", w.Shots)
	case PhaseAuto:
		text = fmt.Sprintf("AUTO を選んでいるため、承認を待たずに、まだ始まっていない Shot %d 件の Reference Pack → 生成を Orchestrator が自動で進めます。生成は有料で、費用がかかります（始めるときに、見積と止め方をチャットでお知らせします）。", w.Shots)
		if w.Stuck > 0 {
			text += fmt.Sprintf("うち %d 件は自動では進められません（Reference Pack の失敗・予算上限・起動の失敗。理由はチャットにあります）。", w.Stuck)
		}
	case PhaseBudgetConfirm:
		text = fmt.Sprintf("チャットで伝えられた予算がまだ確認されていません。予算なしで有料の生成を始めないよう、確認カードに答えるまで、Shot %d 件の Reference Pack → 生成は始めません。", w.Shots)
	default:
		text = fmt.Sprintf("Shot %d 件が PLANNING（Take なし）のまま、生成の開始を待っています。生成は有料なので自動では始まりません。DIRECTOR / MANUAL（または生成の仕組みが未設定）では、Shot ごとに起動します。", w.Shots)
	}
	if gaps := w.Gaps(); len(gaps) > 0 {
		text += "現在は " + strings.Join(gaps, "、") + "です。生成はできますが、顔や見た目の一貫性を保つ参照がありません。"
	}
	return text
}

// NextAction is what to do next.
func (w Wait) NextAction() string {
	switch w.Phase {
	case PhaseDeclined:
		return "次にするもの: 自動で始めるには、チャットに何かメッセージを送ってください。計画を直す（Shot の追加、動画モデルの固定、Quality Policy の変更）と、直した計画で自動で始めます。"
	case PhaseDeclinedUnrecorded:
		return "次にするもの: 自動で始めるには、チャットに何かメッセージを送ってください。この差し戻しのカードには記録が無い（取り込んだ Project など）ため、計画を直しても自動では始まりません。自分で始めるなら、Shot を選んで手動で起動してください。"
	case PhaseAuto:
		if w.Stuck > 0 {
			return "次にするもの: 進められない Shot は、Shot を選んで「Reference Pack」「生成」を押してください。ほかは何も要りません。起動した生成は Job と Progress で追えます。"
		}
		return "次にするもの: ありません。" + BudgetCeilingScope + StopAuto + "起動した生成は Job と Progress で追えます。"
	case PhaseBudgetConfirm:
		return "次にするもの: チャットの「予算 $… で進めますね？」カードで「はい、この内容で進める」か「やめる」を選んでください。答えると、続きから自動で進みます。"
	}
	return "次に押すもの: Scene / Shot で Shot を選び、必要なら先に「Reference Pack」、続けて「生成」を押してください。起動した生成は Job と Progress で追えます。"
}

// Text is Reason and NextAction in one message.
func (w Wait) Text() string { return w.Reason() + "\n" + w.NextAction() }
