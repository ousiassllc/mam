// Package approvalack is what the AI says in the chat when the user answers an Approval card (what the answer does at the card's checkpoint, and no
// more; spec/spec.md §4.4) and when the user changes the MODE or the Confirmation Level, which both the screens and the MCP tools do through the services.
package approvalack

import (
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
)

// approveWords are the replies that count as an approval when the user answers an Approval card in words instead of pressing the button.
// Anything else is a request for changes.
var approveWords = []string{"承認", "承認します", "承認です", "ok", "okay", "はい", "いいね", "いいです", "問題ない", "問題なし", "lgtm", "お願いします"}

// IsApproval reports whether the reply to an Approval card approves it.
func IsApproval(reply string) bool {
	return slices.Contains(approveWords, strings.ToLower(strings.Trim(strings.TrimSpace(reply), "。.!！ ")))
}

// ModeNotice tells that the MODE was switched: AUTO says that it starts the generation by itself (the consent, spec/spec.md §4.3).
func ModeNotice(mode domain.Mode) string {
	if mode == domain.ModeDirector {
		return "DIRECTOR MODE にしました。指示した箇所だけを直して待機します（AI が先に進めることはありません）。"
	}
	return "AUTO MODE に戻しました。この先の工程は AI が続けます。" + mainline.AutoStartNotice
}

// ConfirmationNotice tells that the Confirmation Level was changed, and that no level asks before the generation starts (spec/spec.md §4.3).
func ConfirmationNotice(level domain.ConfirmationLevel, custom []domain.Checkpoint) string {
	switch level {
	case domain.ConfirmFullAuto:
		return "確認レベルを FULL AUTO にしました。Final まで原則確認せずに進めます（判断が必要な場合のみ相談します）。" + mainline.AutoStartAtAnyLevel
	case domain.ConfirmKeyCheck:
		return "確認レベルを KEY CHECK にしました。" + labels(domain.KeyCheckpoints()) + " で確認します。" + mainline.AutoStartAtAnyLevel
	}
	return "確認レベルを CUSTOM CHECK にしました。" + labels(custom) + " で確認します。" + mainline.AutoStartAtAnyLevel
}

func labels(cps []domain.Checkpoint) string {
	out := make([]string, len(cps))
	for i, c := range cps {
		out[i] = c.Label()
	}
	return strings.Join(out, " / ")
}

// Approved tells what an approval does. The generation approval card is the card of v0.1.34: only an AUTO Project goes on by itself, and that is AUTO's
// consent, not the card's; a DIRECTOR Project starts nothing (it never had such a card asked, and the user starts each Shot by hand). unrecorded is a
// generation card that has no record of what it showed (it came with an imported bundle): it is never taken as an approval, which the reply says.
func Approved(card domain.ChatMessage, mode domain.Mode, unrecorded bool) string {
	if cp, _ := domain.CheckpointForRefType(card.RefType); cp != domain.CheckpointGeneration {
		return "承認されました。次の工程に進みます。"
	}
	text := "承認されました。この「生成の承認」は旧方針のカードです。"
	if unrecorded {
		text += "このカードには記録が無い（取り込んだ Project など）ため、承認としては扱いません。"
	}
	if mode != domain.ModeAuto {
		return text + "この Project は AUTO ではないため、生成は自動では始まりません。Shot を選んで手動で起動してください。"
	}
	if unrecorded {
		return text + "AUTO は承認の有無にかかわらず、承認を求めずに、Reference Pack → Shot 生成を自動で進めます。起動した生成は Job と Progress で追えます。"
	}
	return text + "AUTO では承認を求めずに、Reference Pack → Shot 生成を自動で進めます。起動した生成は Job と Progress で追えます。"
}

// ChangesRequested tells what a request for changes does: only the script is redone from the comment (the Writer reads the reply). A Rough Cut or
// Final sends the Project back to EDITING and nothing reads the comment or runs the Editor again, so the user is told to edit. A generation approval
// card of v0.1.34 that is declined keeps the generation from starting by itself and says what to do next: unrecorded is a card with no record, which a
// change of the plan does not lift, and a Project that is not AUTO starts nothing by itself anyway.
func ChangesRequested(card domain.ChatMessage, mode domain.Mode, unrecorded bool) string {
	cp, _ := domain.CheckpointForRefType(card.RefType)
	switch {
	case cp == domain.CheckpointScript:
		return "修正依頼を受け付けました。コメントを反映して脚本を作り直します。"
	case cp == domain.CheckpointGeneration && mode != domain.ModeAuto:
		return mainline.DeclinedAckNotAuto(unrecorded)
	case cp == domain.CheckpointGeneration && unrecorded:
		return mainline.DeclinedAckUnrecorded
	case cp == domain.CheckpointGeneration:
		return mainline.DeclinedAck
	case cp == domain.CheckpointRoughCut, cp == domain.CheckpointFinal:
		return "修正依頼を受け付けました。EDITING に戻しました。コメントは自動では反映されないので、タイムラインエディタで編集してから進めてください。"
	}
	return "修正依頼を受け付けました。"
}

// ResumeState is the state the Project goes to when the user answers the blocking card: the one the checkpoint sets for an approval or for a request for
// changes, and for a card that belongs to no checkpoint Generating (approved) or Planning.
func ResumeState(card domain.ChatMessage, approved bool) domain.ProjectState {
	cp, known := domain.CheckpointForRefType(card.RefType)
	switch {
	case known && approved:
		return cp.ApprovedState()
	case known:
		return cp.ChangesRequestedState()
	case approved:
		return domain.StateGenerating
	}
	return domain.StatePlanning
}
