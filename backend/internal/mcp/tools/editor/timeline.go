package editor

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// TimelineOutput is an Edit Timeline version with its clips (GET /api/v1/projects/{id}/edit-timeline).
type TimelineOutput struct {
	TimelineHead
	Clips  []ClipOut `json:"clips"`
	Limits Limits    `json:"limits"`
}

// ClipOut is a clip of the Edit Timeline with what the editor shows of its Shot.
type ClipOut struct {
	Index         int           `json:"index" jsonschema:"1 始まりの位置（Editor Judge の clip_index と同じ）"`
	ShotID        string        `json:"shot_id"`
	TakeID        string        `json:"take_id"`
	Label         string        `json:"label" jsonschema:"Scene 1 · Shot 2 のような名前"`
	Objective     string        `json:"objective,omitempty"`
	Kind          string        `json:"kind" jsonschema:"main（本編）, insert, reaction（編集で足したカットアウェイ）"`
	Caption       string        `json:"caption,omitempty"`
	InPoint       float64       `json:"in_point" jsonschema:"Take の何秒目から使うか"`
	OutPoint      float64       `json:"out_point" jsonschema:"Take の何秒目まで使うか"`
	SourceSeconds float64       `json:"source_seconds,omitempty" jsonschema:"Take ファイルの長さ"`
	StartSeconds  float64       `json:"start_seconds" jsonschema:"仕上がりの動画でクリップが始まる秒"`
	Transition    TransitionOut `json:"transition"`
}

// TransitionOut is the transition into a clip.
type TransitionOut struct {
	Type    string  `json:"type" jsonschema:"cut, fade, fade_black, dissolve"`
	Seconds float64 `json:"seconds"`
}

// Limits are the bounds the save checks.
type Limits struct {
	MinClipSeconds       float64 `json:"min_clip_seconds"`
	MaxTransitionSeconds float64 `json:"max_transition_seconds"`
	MaxCaptionRunes      int     `json:"max_caption_runes"`
	MaxClips             int     `json:"max_clips"`
}

// NewTimelineOutput turns an Edit Timeline version of the service into the output of the tools.
func NewTimelineOutput(t service.TimelineView) TimelineOutput {
	out := TimelineOutput{
		TimelineHead: newTimelineHead(t),
		Clips:        make([]ClipOut, 0, len(t.Clips)),
		Limits: Limits{
			MinClipSeconds: domain.MinClipSeconds, MaxTransitionSeconds: domain.MaxTransitionSeconds,
			MaxCaptionRunes: domain.MaxCaptionRunes, MaxClips: domain.MaxClips,
		},
	}
	for i, c := range t.Clips {
		out.Clips = append(out.Clips, ClipOut{
			Index: i + 1, ShotID: c.ShotID, TakeID: c.TakeID, Label: c.Label, Objective: c.Objective, Kind: string(clipKind(c.Kind)),
			Caption: c.Caption, InPoint: c.In, OutPoint: c.Out, SourceSeconds: c.SourceSeconds, StartSeconds: c.Start,
			Transition: TransitionOut{Type: string(c.Transition.Type), Seconds: c.Transition.Seconds},
		})
	}
	return out
}

// clipKind is the kind of a clip; a timeline saved before clips had kinds is all story.
func clipKind(k domain.ClipKind) domain.ClipKind {
	if k == "" {
		return domain.ClipMain
	}
	return k
}

func getTimeline(svc *service.EditorService) mcpkit.Handler[ViewInput, TimelineOutput] {
	return func(ctx context.Context, in ViewInput) (TimelineOutput, error) {
		t, err := svc.Timeline(ctx, in.ProjectID, in.BranchID)
		if err != nil {
			return TimelineOutput{}, err
		}
		return NewTimelineOutput(t), nil
	}
}

// SaveInput is the clips to save as the next version of the Edit Timeline.
type SaveInput struct {
	TimelineID string   `json:"timeline_id" jsonschema:"編集の土台にした版の ID（get_edit_timeline の id）。これより新しい版があると conflict"`
	Clips      []ClipIn `json:"clips" jsonschema:"保存するクリップを再生順に。全クリップを渡す（差分ではない）"`
}

// ClipIn is a clip to save.
type ClipIn struct {
	ShotID     string       `json:"shot_id"`
	TakeID     string       `json:"take_id" jsonschema:"その Shot の Take"`
	Kind       string       `json:"kind,omitempty" jsonschema:"main（既定）, insert, reaction"`
	Caption    string       `json:"caption,omitempty" jsonschema:"字幕（60 文字以内）"`
	InPoint    float64      `json:"in_point" jsonschema:"Take の何秒目から使うか"`
	OutPoint   float64      `json:"out_point" jsonschema:"Take の何秒目まで使うか。区間は 0.5 秒以上で Take の長さ以内"`
	Transition TransitionIn `json:"transition,omitempty" jsonschema:"このクリップへ入る Transition（先頭クリップは cut）"`
}

// TransitionIn is the transition into a clip; an empty type is a cut.
type TransitionIn struct {
	Type    string  `json:"type,omitempty" jsonschema:"cut（既定）, fade, fade_black, dissolve"`
	Seconds float64 `json:"seconds,omitempty" jsonschema:"cut 以外では 0 より長く 2 秒以下"`
}

func saveTimeline(svc *service.EditorService) mcpkit.Handler[SaveInput, TimelineOutput] {
	return func(ctx context.Context, in SaveInput) (TimelineOutput, error) {
		tl := domain.EditTimeline{Clips: make([]domain.EditClip, len(in.Clips))}
		for i, c := range in.Clips {
			tl.Clips[i] = domain.EditClip{
				ShotID: c.ShotID, TakeID: c.TakeID, Kind: domain.ClipKind(c.Kind), Caption: c.Caption, In: c.InPoint, Out: c.OutPoint,
				Transition: domain.Transition{Type: domain.TransitionType(c.Transition.Type), Seconds: c.Transition.Seconds},
			}
		}
		saved, err := svc.SaveTimeline(ctx, in.TimelineID, tl)
		if err != nil {
			return TimelineOutput{}, err
		}
		return NewTimelineOutput(saved), nil
	}
}
