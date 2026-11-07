// Package approvalack is what the AI says in the chat when the user answers an Approval card: what the answer does at the card's checkpoint, and no
// more (spec/spec.md §4.4).
package approvalack

import (
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
)

// Approved tells what an approval does.
func Approved(card domain.ChatMessage) string {
	if cp, _ := domain.CheckpointForRefType(card.RefType); cp == domain.CheckpointGeneration {
		return "承認されました。Reference Pack → Shot 生成を、自動で進めます。起動した生成は Job と Progress で追えます。"
	}
	return "承認されました。次の工程に進みます。"
}

// ChangesRequested tells what a request for changes does: only the script is redone from the comment (the Writer reads the reply). A Rough Cut or
// Final sends the Project back to EDITING and nothing reads the comment or runs the Editor again, so the user is told to edit. A generation approval
// that is declined starts nothing and says what to do next.
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
