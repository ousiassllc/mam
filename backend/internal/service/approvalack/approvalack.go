// Package approvalack is what the AI says in the chat when the user answers an Approval card (what the answer does at the card's checkpoint, and no
// more; spec/spec.md §4.4) and when the user changes the MODE or the Confirmation Level, which both the screens and the MCP tools do through the services.
package approvalack

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
)

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

// Approved tells what an approval does.
func Approved(card domain.ChatMessage) string {
	if cp, _ := domain.CheckpointForRefType(card.RefType); cp == domain.CheckpointGeneration {
		return "承認されました。この「生成の承認」は旧方針のカードです。AUTO では承認を求めずに、Reference Pack → Shot 生成を自動で進めます。起動した生成は Job と Progress で追えます。"
	}
	return "承認されました。次の工程に進みます。"
}

// ChangesRequested tells what a request for changes does: only the script is redone from the comment (the Writer reads the reply). A Rough Cut or
// Final sends the Project back to EDITING and nothing reads the comment or runs the Editor again, so the user is told to edit. A generation approval
// card of v0.1.34 that is declined keeps the generation from starting by itself and says what to do next.
func ChangesRequested(card domain.ChatMessage) string {
	cp, _ := domain.CheckpointForRefType(card.RefType)
	switch cp {
	case domain.CheckpointScript:
		return "修正依頼を受け付けました。コメントを反映して脚本を作り直します。"
	case domain.CheckpointGeneration:
		return mainline.DeclinedAck
	case domain.CheckpointRoughCut, domain.CheckpointFinal:
		return "修正依頼を受け付けました。EDITING に戻しました。コメントは自動では反映されないので、タイムラインエディタで編集してから進めてください。"
	}
	return "修正依頼を受け付けました。"
}
