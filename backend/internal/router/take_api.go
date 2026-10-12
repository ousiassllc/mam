package router

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// takeScoresBody is the four scores of a Take, each 0-100, and their mean.
type takeScoresBody struct {
	Product    float64 `json:"product"`
	Acting     float64 `json:"acting"`
	Camera     float64 `json:"camera"`
	Continuity float64 `json:"continuity"`
	Overall    float64 `json:"overall"`
}

// takeBody is a Take of the comparison. The URLs are the ones the component uses: it never builds
// one itself (HATEOAS). The action links (select_url, reject_url) and partial_usable exist only while
// the server allows that action on the Take: the component shows an action iff its link is there.
type takeBody struct {
	ID              string          `json:"id"`
	Label           string          `json:"label" doc:"Name in the comparison: A, B, ..."`
	Status          string          `json:"status" enum:"candidate,ai_pick,selected,rejected"`
	StatusLabel     string          `json:"status_label" doc:"The status as it is named on screen (AI Pick / 採用 / 除外 / 候補), the same as the Shot card"`
	Adopted         bool            `json:"adopted" doc:"true for the Take the Shot uses"`
	ModelName       string          `json:"model_name"`
	Resolution      string          `json:"resolution,omitempty"`
	FPS             float64         `json:"fps,omitempty"`
	DurationSeconds float64         `json:"duration_seconds,omitempty"`
	Verdict         string          `json:"verdict,omitempty" doc:"Latest AI Video Judge verdict: pass or fail"`
	Scores          *takeScoresBody `json:"scores" doc:"null until the Take has been judged"`
	Proxied         bool            `json:"proxied" doc:"true when the preview plays a 1080p proxy of a 4K file"`
	VideoURL        string          `json:"video_url,omitempty" doc:"empty when the file was archived"`
	ThumbnailURL    string          `json:"thumbnail_url,omitempty"`
	JudgeResultURL  string          `json:"judge_result_url"`
	SelectURL       string          `json:"select_url,omitempty" doc:"POST here to select the Take; present only while it can be selected (not rejected, not archived, not already selected)"`
	RejectURL       string          `json:"reject_url,omitempty" doc:"DELETE here to reject the Take; present only while it can be rejected (not rejected, not archived)"`
	PartialUsable   bool            `json:"partial_usable" doc:"true when parts of this Take can be picked for Partial Take Use"`
	CreatedAt       time.Time       `json:"created_at"`
}

type takeComparisonBody struct {
	ShotID          string         `json:"shot_id"`
	DurationSeconds float64        `json:"duration_seconds,omitempty" doc:"Length planned for the Shot"`
	PartialURL      string         `json:"partial_url,omitempty" doc:"POST here to join parts of the Takes (Partial Take Use); present only while the Shot can be composed"`
	Limits          takeLimitsBody `json:"limits"`
	Takes           []takeBody     `json:"takes"`
}

type takeLimitsBody struct {
	MaxSegments       int     `json:"max_segments"`
	MinSegmentSeconds float64 `json:"min_segment_seconds"`
	MinAxisScore      int     `json:"min_axis_score" doc:"An axis score below this is one the AI Video Judge fails: the Quality Policy of the Project with the user's override. The component shows such a score as low"`
}

func newTakeBody(v service.TakeView) takeBody {
	b := takeBody{
		ID: v.ID, Label: v.Label, Status: string(v.Status), StatusLabel: v.Status.Label(), Adopted: v.Adopted, ModelName: v.ModelName,
		Resolution: v.Resolution, FPS: v.FPS, DurationSeconds: v.DurationSeconds, Verdict: string(v.Verdict),
		Proxied: v.Proxied(), JudgeResultURL: paths.APITake(v.ID, "judge-result"), CreatedAt: v.CreatedAt, PartialUsable: v.Usable(),
	}
	if v.Selectable() {
		b.SelectURL = paths.Take(v.ID, "select")
	}
	if v.Usable() {
		b.RejectURL = paths.Take(v.ID)
	}
	if v.FilePath != "" {
		b.VideoURL, b.ThumbnailURL = domain.TakeVideoURL(v.ID), domain.TakeThumbnailURL(v.ID)
	}
	if s := v.Scores; s != nil {
		b.Scores = &takeScoresBody{Product: s.Product, Acting: s.Acting, Camera: s.Camera, Continuity: s.Continuity, Overall: s.Overall()}
	}
	return b
}

// registerTakeAPI registers GET /api/v1/shots/{id}/takes: the Takes of a Shot with their scores
// and status, for mam-take-comparator.
func registerTakeAPI(api huma.API, services *service.Services) {
	type output struct{ Body takeComparisonBody }
	huma.Register(api, huma.Operation{
		OperationID: "get-shot-takes",
		Method:      "GET",
		Path:        APIPrefix + "/shots/{id}/takes",
		Summary:     "Takes of a Shot with their scores (Product / Acting / Camera / Continuity) and status",
		Tags:        []string{"takes"},
		Errors:      []int{404},
	}, func(ctx context.Context, in *struct {
		ID string `path:"id" doc:"Shot ID"`
	}) (*output, error) {
		cmp, err := services.Takes.Compare(ctx, in.ID)
		if err != nil {
			if isNotFound(err) {
				return nil, huma.Error404NotFound("Shot が見つかりません")
			}
			return nil, err
		}
		policy, err := services.Policy.Policy(ctx, cmp.Project.ID)
		if err != nil {
			return nil, err
		}
		limits := takeLimitsBody{MaxSegments: domain.MaxTakeSegments, MinSegmentSeconds: domain.MinTakeSegmentSeconds, MinAxisScore: policy.Effective.MinAxisScore}
		body := takeComparisonBody{ShotID: cmp.Shot.ID, DurationSeconds: cmp.Shot.Duration, Limits: limits, Takes: make([]takeBody, len(cmp.Takes))}
		if cmp.PartialUse {
			body.PartialURL = paths.Shot(cmp.Shot.ID, "takes", "partial")
		}
		for i, t := range cmp.Takes {
			body.Takes[i] = newTakeBody(t)
		}
		return &output{Body: body}, nil
	})
}
