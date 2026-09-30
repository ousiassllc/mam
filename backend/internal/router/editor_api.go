package router

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

type transitionBody struct {
	Type    string  `json:"type" enum:"cut,fade,fade_black,dissolve"`
	Seconds float64 `json:"seconds"`
}

type editClipBody struct {
	ShotID        string         `json:"shot_id"`
	TakeID        string         `json:"take_id"`
	Label         string         `json:"label" doc:"Scene / Shot position, e.g. Scene 1 · Shot 2"`
	Kind          string         `json:"kind" enum:"main,insert,reaction" doc:"main is the story; insert / reaction are cutaways added in editing"`
	Caption       string         `json:"caption,omitempty" doc:"Text drawn over the clip"`
	Objective     string         `json:"objective,omitempty"`
	SourceSeconds float64        `json:"source_seconds,omitempty" doc:"Length of the Take file"`
	InPoint       float64        `json:"in_point"`
	OutPoint      float64        `json:"out_point"`
	Start         float64        `json:"start" doc:"Second the clip begins at in the video"`
	Transition    transitionBody `json:"transition" doc:"How the clip follows the one before it"`
	VideoURL      string         `json:"video_url,omitempty"`
	ThumbnailURL  string         `json:"thumbnail_url,omitempty"`
}

type editorFindingBody struct {
	ClipIndex int           `json:"clip_index" doc:"1-based; 0 is the timeline as a whole"`
	Issue     string        `json:"issue"`
	Action    string        `json:"action"`
	NeedShot  *shotNeedBody `json:"need_shot,omitempty" doc:"The additional Shot only new footage settles (verdict need_shot)"`
}

type shotNeedBody struct {
	Kind            string  `json:"kind" enum:"insert,reaction"`
	AfterClip       int     `json:"after_clip" doc:"1-based clip the new Shot follows"`
	Objective       string  `json:"objective"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type editorJudgeBody struct {
	Verdict   string              `json:"verdict" enum:"pass,re_edit,need_shot"`
	Findings  []editorFindingBody `json:"findings"`
	CreatedAt time.Time           `json:"created_at"`
}

type editTimelineBody struct {
	ID             string           `json:"id" doc:"The version shown; the editor saves against it"`
	VersionNumber  int              `json:"version_number"`
	CreatedAt      time.Time        `json:"created_at"`
	Duration       float64          `json:"duration_seconds"`
	Clips          []editClipBody   `json:"clips"`
	Judge          *editorJudgeBody `json:"judge,omitempty" doc:"Latest Editor Judge result of this version"`
	BranchID       string           `json:"branch_id,omitempty" doc:"The Branch this timeline belongs to; empty for the trunk"`
	SaveURL        string           `json:"save_url" doc:"PUT the clips here (action route)"`
	RequestShotURL string           `json:"request_shot_url" doc:"POST here to ask the Director for the additional Shots (verdict need_shot)"`
	PreviewURL     string           `json:"preview_url" doc:"The rendered Rough Cut (video/mp4)"`
	Transitions    []string         `json:"transitions"`
	Limits         editorLimitsBody `json:"limits"`
	Readonly       bool             `json:"readonly" doc:"true once the Project has moved past editing"`
}

type editorLimitsBody struct {
	MinClipSeconds       float64 `json:"min_clip_seconds"`
	MaxTransitionSeconds float64 `json:"max_transition_seconds"`
	MaxClips             int     `json:"max_clips"`
	MaxCaptionRunes      int     `json:"max_caption_runes"`
}

func newEditTimelineBody(v service.TimelineView) editTimelineBody {
	b := editTimelineBody{
		ID: v.ID, VersionNumber: v.VersionNumber, CreatedAt: v.CreatedAt, Duration: v.Duration,
		Clips:          make([]editClipBody, len(v.Clips)),
		BranchID:       v.BranchID,
		SaveURL:        "/edit-timelines/" + v.ID,
		RequestShotURL: "/edit-timelines/" + v.ID + "/request-additional-shot",
		PreviewURL:     "/edit-timelines/" + v.ID + "/preview",
		Limits:         editorLimitsBody{MinClipSeconds: domain.MinClipSeconds, MaxTransitionSeconds: domain.MaxTransitionSeconds, MaxClips: domain.MaxClips, MaxCaptionRunes: domain.MaxCaptionRunes},
		Readonly:       !v.Editable,
	}
	for _, t := range domain.TransitionTypes() {
		b.Transitions = append(b.Transitions, string(t))
	}
	for i, c := range v.Clips {
		b.Clips[i] = editClipBody{
			ShotID: c.ShotID, TakeID: c.TakeID, Kind: clipKind(c.Kind), Caption: c.Caption, Label: c.Label, Objective: c.Objective, SourceSeconds: c.SourceSeconds,
			InPoint: c.In, OutPoint: c.Out, Start: c.Start, Transition: transitionBody{Type: string(c.Transition.Type), Seconds: c.Transition.Seconds},
			VideoURL: c.VideoURL, ThumbnailURL: c.ThumbnailURL,
		}
	}
	if j := v.Judge; j != nil {
		jb := &editorJudgeBody{Verdict: string(j.Verdict), CreatedAt: j.CreatedAt, Findings: make([]editorFindingBody, len(j.Findings))}
		for i, f := range j.Findings {
			jb.Findings[i] = editorFindingBody{ClipIndex: f.ClipIndex, Issue: f.Issue, Action: f.Action}
			if n := f.Need; n != nil {
				jb.Findings[i].NeedShot = &shotNeedBody{Kind: string(n.Kind), AfterClip: n.AfterClip, Objective: n.Objective, DurationSeconds: n.Seconds()}
			}
		}
		b.Judge = jb
	}
	return b
}

// registerEditorAPI registers GET /api/v1/projects/{id}/edit-timeline: the latest Edit Timeline
// for mam-timeline-editor. The editor saves through PUT /edit-timelines/:id (an action route).
func registerEditorAPI(api huma.API, services *service.Services) {
	type output struct {
		Body editTimelineBody
	}
	huma.Register(api, huma.Operation{
		OperationID: "get-edit-timeline",
		Method:      "GET",
		Path:        APIPrefix + "/projects/{id}/edit-timeline",
		Summary:     "Latest Edit Timeline of a Project (Shot order, Cut Points, Transitions)",
		Tags:        []string{"editor"},
		Errors:      []int{404},
	}, func(ctx context.Context, in *struct {
		ID       string `path:"id" doc:"Project ID"`
		BranchID string `query:"branch_id" doc:"Branch whose Edit Timeline to read; omitted is the trunk"`
	}) (*output, error) {
		v, err := services.Editor.Timeline(ctx, in.ID, in.BranchID)
		if err != nil {
			if isNotFound(err) {
				return nil, huma.Error404NotFound("Edit Timeline がありません。Rough Cut を作成してください")
			}
			return nil, err
		}
		return &output{Body: newEditTimelineBody(v)}, nil
	})
}

// clipKind is the kind of a clip; a timeline saved before clips had kinds is all story.
func clipKind(k domain.ClipKind) string {
	if k == "" {
		return string(domain.ClipMain)
	}
	return string(k)
}
