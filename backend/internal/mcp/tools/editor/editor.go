// Package editor holds the MCP tools of the editor screen that answer at once: the editing state, the
// Edit Timeline (read and save), Rough Cut, Editor → Reshoot Loop request, Final QC, voice cloning and
// the preview / export download URLs. The stages that take long (Editor Judge, audio, export) are in
// ../editorstage and return a job_id.
package editor

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools, the upload purpose and the download kinds of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	registerFiles(r, svc.Editor)

	mcpkit.Read(r, "get_editor_view",
		"編集画面の状態（Project の状態・最新の Edit Timeline の概要と Editor Judge・音声・声の複製・Final QC・書き出し一覧・書き出しの既定値）/ Editing state of a Project: timeline, judge, audio, voices, Final QC and exports. 画面の GET /projects/:id/edit と同じ内容。",
		getView(svc.Editor))
	mcpkit.Read(r, "get_edit_timeline",
		"最新の Edit Timeline の全クリップ（Shot の順序・Cut Point・Transition・字幕・各クリップの開始秒）/ Latest Edit Timeline with every clip. Rough Cut 前は not_found。",
		getTimeline(svc.Editor))
	mcpkit.Write(r, "save_edit_timeline",
		"クリップ列を Edit Timeline の次の版として保存する。timeline_id が最新でなければ conflict、Take が Shot のものでない・区間が 0.5 秒未満・Take の長さ超過は validation。保存すると Project は EDITING に戻る / Save clips as the next Edit Timeline version.",
		saveTimeline(svc.Editor))
	mcpkit.Write(r, "create_rough_cut",
		"全 Shot の Selected Take から Rough Cut を作って EDITING にする（Production Template に合わせた編集つき。Editing が MANUAL / LOCK の Shot の手編集は残る）/ Make the Rough Cut from the Selected Takes. 画面の「Rough Cut を作る」と同じ。",
		roughCut(svc.Editor))
	mcpkit.Write(r, "request_additional_shot",
		"Editor Judge が素材不足と判定した版について、Director に追加 Shot を要求して生成を始める（Editor → Reshoot Loop。Retry Policy の上限まで）。Judge 前は conflict / Request the additional Shots the Editor Judge asked for.",
		requestShot(svc.Editor), mcpkit.OpenWorld())
	mcpkit.Write(r, "run_final_qc",
		"Final QC（最新の Edit / Audio Timeline と Brand 制約の検査。常に本線を検査し、branch_id は応答に載せる Branch だけを決める）を実行する。PASS は READY（Final checkpoint があれば承認待ち）、FAIL は EDITING か AUDIO に戻る。Project が AUDIO のときだけ / Run Final QC.",
		finalQC(svc.Editor))
	mcpkit.Read(r, "list_voice_clones",
		"Project の話者ごとの複製した声（話者名・名前）。複製は voice_sample のアップロード / Cloned voices of the Project's speakers.",
		listVoices(svc.Editor))
	mcpkit.Read(r, "get_edit_preview_url",
		"Edit Timeline の版のレンダリング済み Rough Cut（video/mp4）のダウンロード URL（15 分有効）。初回はレンダリングするので時間がかかる / Download URL of the rendered Rough Cut of a timeline version.",
		previewURL(r))
	mcpkit.Read(r, "get_export_download_url",
		"書き出した動画のダウンロード URL（15 分有効）/ Download URL of an exported video.",
		exportURL(r))
}

func getView(svc *service.EditorService) mcpkit.Handler[ViewInput, ViewOutput] {
	return func(ctx context.Context, in ViewInput) (ViewOutput, error) {
		v, err := svc.View(ctx, in.ProjectID, in.BranchID)
		if err != nil {
			return ViewOutput{}, err
		}
		return NewViewOutput(v), nil
	}
}

func roughCut(svc *service.EditorService) mcpkit.Handler[ViewInput, ViewOutput] {
	return func(ctx context.Context, in ViewInput) (ViewOutput, error) {
		v, err := svc.RoughCut(ctx, in.ProjectID, in.BranchID)
		if err != nil {
			return ViewOutput{}, err
		}
		return NewViewOutput(v), nil
	}
}

// TimelineInput names an Edit Timeline version.
type TimelineInput struct {
	TimelineID string `json:"timeline_id" jsonschema:"Edit Timeline の版の ID（get_edit_timeline / get_editor_view の timeline.id）"`
}

func requestShot(svc *service.EditorService) mcpkit.Handler[TimelineInput, ViewOutput] {
	return func(ctx context.Context, in TimelineInput) (ViewOutput, error) {
		v, err := svc.RequestAdditionalShot(ctx, in.TimelineID)
		if err != nil {
			return ViewOutput{}, err
		}
		return NewViewOutput(v), nil
	}
}

func finalQC(svc *service.EditorService) mcpkit.Handler[ViewInput, ViewOutput] {
	return func(ctx context.Context, in ViewInput) (ViewOutput, error) {
		v, err := svc.FinalQC(ctx, in.ProjectID, in.BranchID)
		if err != nil {
			return ViewOutput{}, err
		}
		return NewViewOutput(v), nil
	}
}
