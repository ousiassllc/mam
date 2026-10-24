// Package takes holds the MCP tools of the Takes of a Shot: the comparison, selecting, rejecting, Partial
// Take Use, the video and thumbnail downloads and the Generation Traceability.
package takes

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotview"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package and the download kinds take_video and take_thumbnail.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	registerDownloads(r.Files, svc.Takes)

	mcpkit.Read(r, "list_shot_takes",
		"Shot の Take の比較（スコア・状態・採用・部分採用の可否・上限）。画面の Take 比較と GET /api/v1/shots/:id/takes と同じ / The Takes of a Shot side by side.",
		listTakes(svc.Takes, svc.Policy))
	mcpkit.Write(r, "select_take",
		"Take を Shot の採用 Take にする（AI Pick や以前の選択を置き換える）。除外済み・アーカイブ済み・選択済みは validation / Make a Take the one its Shot uses.",
		selectTake(svc.Takes))
	mcpkit.Destroy(r, "reject_take",
		"Take を除外する（ファイルと履歴は残る。除外の取り消しは無い）/ Reject (exclude) a Take.",
		rejectTake(svc.Takes))
	mcpkit.Write(r, "use_partial_takes",
		"複数の Take の区間をつないで新しい Take を作り、Shot の Take にする（Partial Take Use。segments か instruction）/ Join parts of the Takes into a new Take.",
		usePartial(svc.Takes))
	mcpkit.Read(r, "get_take_video_url",
		"Take のプレビュー動画のダウンロード URL（4K は 1080p プロキシ。15 分有効）。アーカイブ済みは not_found / Download URL of a Take's preview video.",
		downloadURL(r.Files, KindTakeVideo))
	mcpkit.Read(r, "get_take_thumbnail_url",
		"Take の先頭フレームの JPEG のダウンロード URL（15 分有効。ffmpeg が無いと unavailable）/ Download URL of a Take's thumbnail.",
		downloadURL(r.Files, KindTakeThumbnail))
	mcpkit.Read(r, "get_take_trace",
		"Take の Generation Traceability（元の Shot Spec・Prompt・World State 版・モデルとパラメータ・Reference・出力・Judge・Repair）/ Everything that led to a Take.",
		getTrace(svc.Trace))
}

// Scores are the four axes of the AI Video Judge, 0-100.
type Scores struct {
	Product    float64 `json:"product"`
	Acting     float64 `json:"acting"`
	Camera     float64 `json:"camera"`
	Continuity float64 `json:"continuity"`
	Overall    float64 `json:"overall"`
}

// Take is one Take of the comparison.
type Take struct {
	ID              string    `json:"id"`
	Label           string    `json:"label" jsonschema:"比較での名前（A, B ...）。use_partial_takes の instruction で使う"`
	Status          string    `json:"status" jsonschema:"candidate, ai_pick, selected or rejected"`
	StatusLabel     string    `json:"status_label"`
	Adopted         bool      `json:"adopted" jsonschema:"Shot が使っている Take"`
	ModelName       string    `json:"model_name"`
	Resolution      string    `json:"resolution,omitempty"`
	FPS             float64   `json:"fps,omitempty"`
	DurationSeconds float64   `json:"duration_seconds,omitempty"`
	Verdict         string    `json:"verdict,omitempty" jsonschema:"最新の Judge の判定: pass or fail"`
	Scores          *Scores   `json:"scores,omitempty" jsonschema:"Judge 済みのとき"`
	Proxied         bool      `json:"proxied" jsonschema:"プレビューが 4K の 1080p プロキシか"`
	HasVideo        bool      `json:"has_video" jsonschema:"false ならアーカイブ済み"`
	Selectable      bool      `json:"selectable" jsonschema:"今 select_take できる"`
	Rejectable      bool      `json:"rejectable" jsonschema:"今 reject_take できる"`
	PartialUsable   bool      `json:"partial_usable" jsonschema:"部分採用の元にできる"`
	CreatedAt       time.Time `json:"created_at"`
}

// Limits are the limits of Partial Take Use and of the score the Judge fails by.
type Limits struct {
	MaxSegments       int     `json:"max_segments"`
	MinSegmentSeconds float64 `json:"min_segment_seconds"`
	MinAxisScore      int     `json:"min_axis_score" jsonschema:"これ未満の軸のスコアは Judge が fail にする（Quality Policy とユーザーの上書き）"`
}

// ListTakesInput names the Shot.
type ListTakesInput struct {
	ShotID string `json:"shot_id"`
}

// ListTakesOutput is the comparison.
type ListTakesOutput struct {
	ShotID          string  `json:"shot_id"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	PartialUse      bool    `json:"partial_use" jsonschema:"Partial Take Use ができるか（ffmpeg があり、切れる Take がある）"`
	Limits          Limits  `json:"limits"`
	Takes           []Take  `json:"takes" jsonschema:"古い順"`
}

func listTakes(svc *service.TakeService, policies *service.PolicyService) mcpkit.Handler[ListTakesInput, ListTakesOutput] {
	return func(ctx context.Context, in ListTakesInput) (ListTakesOutput, error) {
		cmp, err := svc.Compare(ctx, in.ShotID)
		if err != nil {
			return ListTakesOutput{}, err
		}
		policy, err := policies.Policy(ctx, cmp.Project.ID)
		if err != nil {
			return ListTakesOutput{}, err
		}
		out := ListTakesOutput{
			ShotID: cmp.Shot.ID, DurationSeconds: cmp.Shot.Duration, PartialUse: cmp.PartialUse, Takes: make([]Take, len(cmp.Takes)),
			Limits: Limits{MaxSegments: domain.MaxTakeSegments, MinSegmentSeconds: domain.MinTakeSegmentSeconds, MinAxisScore: policy.Effective.MinAxisScore},
		}
		for i, t := range cmp.Takes {
			out.Takes[i] = takeOf(t)
		}
		return out, nil
	}
}

func takeOf(v service.TakeView) Take {
	t := Take{
		ID: v.ID, Label: v.Label, Status: string(v.Status), StatusLabel: v.Status.Label(), Adopted: v.Adopted, ModelName: v.ModelName,
		Resolution: v.Resolution, FPS: v.FPS, DurationSeconds: v.DurationSeconds, Verdict: string(v.Verdict), Proxied: v.Proxied(),
		HasVideo: v.FilePath != "", Selectable: v.Selectable(), Rejectable: v.Usable(), PartialUsable: v.Usable(), CreatedAt: v.CreatedAt,
	}
	if s := v.Scores; s != nil {
		t.Scores = &Scores{Product: s.Product, Acting: s.Acting, Camera: s.Camera, Continuity: s.Continuity, Overall: s.Overall()}
	}
	return t
}

// TakeInput names a Take.
type TakeInput struct {
	TakeID string `json:"take_id"`
}

// ShotOutput is the Shot after the action.
type ShotOutput struct {
	Shot shotview.Card `json:"shot"`
}

func selectTake(svc *service.TakeService) mcpkit.Handler[TakeInput, ShotOutput] {
	return func(ctx context.Context, in TakeInput) (ShotOutput, error) {
		frag, err := svc.Select(ctx, in.TakeID)
		return ShotOutput{Shot: shotview.FromFragment(frag)}, err
	}
}

func rejectTake(svc *service.TakeService) mcpkit.Handler[TakeInput, ShotOutput] {
	return func(ctx context.Context, in TakeInput) (ShotOutput, error) {
		frag, err := svc.Reject(ctx, in.TakeID)
		return ShotOutput{Shot: shotview.FromFragment(frag)}, err
	}
}

// PartialInput is Partial Take Use: the parts as data, or in words.
type PartialInput struct {
	ShotID      string               `json:"shot_id"`
	Segments    []domain.TakeSegment `json:"segments,omitempty" jsonschema:"つなぐ区間（take_id, start, end の秒）。instruction より優先"`
	Instruction string               `json:"instruction,omitempty" jsonschema:"言葉での指定（例: Take A の 0-2秒 + Take B の 2-4秒）"`
}

func usePartial(svc *service.TakeService) mcpkit.Handler[PartialInput, ShotOutput] {
	return func(ctx context.Context, in PartialInput) (ShotOutput, error) {
		frag, err := svc.UsePartial(ctx, in.ShotID, service.PartialInput{Segments: in.Segments, Instruction: in.Instruction})
		return ShotOutput{Shot: shotview.FromFragment(frag)}, err
	}
}
