// Package editorstage holds the MCP tools of the editing stages that take long: the Editor Judge, the
// audio generation and the export. The service layer runs each of them to the end inside one call (the
// screen waits for it), so a tool starts it in the background and answers with a job_id at once; get_job
// then reports the progress and the result. The checks that refuse a request (wrong state, another stage
// running, a bad option, the feature off) are answered as the error of the call, as on the screen.
// Rough Cut and Final QC are quick and answer directly (../editor).
package editorstage

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Job kinds of the stages.
const (
	KindJudgeTimeline = "judge_edit_timeline"
	KindGenerateAudio = "generate_audio"
	KindExportVideo   = "export_video"
)

// Register adds the tools and the job kinds of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	t := newTracker(r)
	r.Jobs.RegisterKind(KindJudgeTimeline, t.probe("Editor Judge が判定しています"), mcpkit.InMemory())
	r.Jobs.RegisterKind(KindGenerateAudio, t.probe("音声（セリフ・BGM・効果音）を作ってミックスしています"), mcpkit.InMemory())
	r.Jobs.RegisterKind(KindExportVideo, t.probe("動画を書き出しています"), mcpkit.InMemory())

	mcpkit.Write(r, "judge_edit_timeline",
		"Edit Timeline の版を Editor Judge にかける（job_id を返す）。PASS は音声制作（AUDIO）へ、re_edit は EDITING のまま指摘がチャットに出る、need_shot は request_additional_shot で追加 Shot を頼める。timeline_id が最新でない・EDITING でないと conflict / Run the Editor Judge on a timeline version; returns a job_id.",
		judgeTimeline(t, svc.Editor), mcpkit.OpenWorld())
	mcpkit.Write(r, "generate_audio",
		"Audio Timeline を作る（job_id を返す）。セリフ・ナレーションを読み上げ、Quality Policy が許す範囲で BGM・環境音・効果音・Foley・Lip Sync を作ってミックスし、ラウドネスを揃える。コストがかかる。Project が AUDIO のときだけ / Generate the Audio Timeline; returns a job_id.",
		generateAudio(t, svc.Editor), mcpkit.OpenWorld())
	mcpkit.Write(r, "export_video",
		"完成動画を書き出す（job_id を返す）。aspect_ratio・resolution・codec を省略すると Brief の縦横比・1080p・H.264。Project が READY か EXPORTED（再書き出し）のときだけ。最新の編集に対する音声が無いと conflict。完成後は get_export_download_url / Export the finished video; returns a job_id.",
		exportVideo(t, svc.Editor))
}

// Ref is what a tool that starts a stage answers with.
type Ref struct {
	mcpkit.JobRef
}

// JudgeInput names the timeline version to judge.
type JudgeInput struct {
	TimelineID string `json:"timeline_id" jsonschema:"Edit Timeline の最新の版の ID（get_editor_view の timeline.id）"`
}

func judgeTimeline(t *tracker, svc *service.EditorService) mcpkit.Handler[JudgeInput, Ref] {
	return func(ctx context.Context, in JudgeInput) (Ref, error) {
		ref, err := t.start(ctx, KindJudgeTimeline, in.TimelineID, func(ctx context.Context) (map[string]any, error) {
			v, err := svc.JudgeTimeline(ctx, in.TimelineID)
			if err != nil {
				return nil, err
			}
			res := map[string]any{"project_id": v.Project.ID, "project_state": string(v.Project.State), "pending_shots": v.PendingShots}
			if v.Timeline != nil && v.Timeline.Judge != nil {
				res["verdict"] = string(v.Timeline.Judge.Verdict)
				res["findings"] = len(v.Timeline.Judge.Findings)
			}
			return res, nil
		})
		return Ref{JobRef: ref}, err
	}
}

// AudioInput names the Project (and the Branch) the audio is made for.
type AudioInput struct {
	ProjectID string `json:"project_id"`
	BranchID  string `json:"branch_id,omitempty" jsonschema:"音声を作る Branch の ID。省略すると本線（trunk）"`
}

func generateAudio(t *tracker, svc *service.EditorService) mcpkit.Handler[AudioInput, Ref] {
	return func(ctx context.Context, in AudioInput) (Ref, error) {
		ref, err := t.start(ctx, KindGenerateAudio, in.ProjectID, func(ctx context.Context) (map[string]any, error) {
			v, err := svc.GenerateAudio(ctx, in.ProjectID, in.BranchID)
			if err != nil {
				return nil, err
			}
			res := map[string]any{"project_id": v.Project.ID, "project_state": string(v.Project.State)}
			if a := v.Audio; a != nil {
				res["audio_timeline_id"], res["version_number"] = a.ID, a.VersionNumber
				res["speech_lines"], res["measured_lufs"], res["skipped_steps"] = a.Timeline.SpeechLines(), a.Timeline.MeasuredLUFS, len(a.Timeline.Skipped)
			}
			if len(v.ProviderNotes) > 0 {
				res["provider_notes"] = v.ProviderNotes
			}
			return res, nil
		})
		return Ref{JobRef: ref}, err
	}
}

// ExportInput is the choices of an export; a field left empty takes the default.
type ExportInput struct {
	ProjectID   string `json:"project_id"`
	BranchID    string `json:"branch_id,omitempty" jsonschema:"書き出す Branch の ID。省略すると本線（trunk）"`
	AspectRatio string `json:"aspect_ratio,omitempty" jsonschema:"16:9, 9:16, 1:1, 4:5。省略すると Brief の縦横比"`
	Resolution  string `json:"resolution,omitempty" jsonschema:"720p, 1080p, 1440p, 2160p（短辺）。省略すると 1080p"`
	Codec       string `json:"codec,omitempty" jsonschema:"h264, h265, vp9（WebM）。省略すると h264"`
}

func exportVideo(t *tracker, svc *service.EditorService) mcpkit.Handler[ExportInput, Ref] {
	return func(ctx context.Context, in ExportInput) (Ref, error) {
		opts := service.ExportInput{BranchID: in.BranchID, AspectRatio: in.AspectRatio, Resolution: in.Resolution, Codec: in.Codec}
		ref, err := t.start(ctx, KindExportVideo, in.ProjectID, func(ctx context.Context) (map[string]any, error) {
			v, err := svc.Export(ctx, in.ProjectID, opts)
			if err != nil {
				return nil, err
			}
			res := map[string]any{"project_id": v.Project.ID, "project_state": string(v.Project.State)}
			if len(v.Exports) > 0 { // newest first: the one this run made
				e := v.Exports[0]
				res["export_id"], res["aspect_ratio"], res["resolution"], res["codec"] = e.ID, e.AspectRatio, e.Resolution, e.Codec
			}
			return res, nil
		})
		return Ref{JobRef: ref}, err
	}
}
