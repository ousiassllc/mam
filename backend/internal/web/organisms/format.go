// Package organisms holds the page sections of the chat: thread, composer,
// workspace, project list and Brief form.
package organisms

import "github.com/ousiassllc/mam/backend/internal/domain"

// AcceptedUploads is the accept attribute of file inputs (service/upload.go validates for real).
const AcceptedUploads = ".jpg,.jpeg,.png,.webp,.gif,.mp4,.mov,.webm"

func stateLabel(s domain.ProjectState) string {
	switch s {
	case domain.StateDraft:
		return "下書き（Brief 入力中）"
	case domain.StatePlanning:
		return "企画・脚本"
	case domain.StateVisualDesign:
		return "ビジュアル設計"
	case domain.StateAwaitingApproval:
		return "確認待ち"
	case domain.StateGenerating:
		return "生成中"
	case domain.StateJudging:
		return "Judge 中"
	case domain.StateRepairing:
		return "Repair 中"
	case domain.StateEditing:
		return "編集中"
	case domain.StateEditJudging:
		return "編集チェック中"
	case domain.StateAudio:
		return "音声制作"
	case domain.StateFinalQC:
		return "Final QC"
	case domain.StateReady:
		return "書き出し可能"
	case domain.StateExported:
		return "書き出し済み"
	}
	return string(s)
}
