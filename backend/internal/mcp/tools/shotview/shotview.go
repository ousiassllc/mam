// Package shotview is the Shot card of the MCP tools of the Scene / Shot / Take domain: the data a screen's
// Shot card shows, as the output of the tools that read or change a Shot. It registers no tool.
package shotview

import (
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Take is one Take on a Shot card.
type Take struct {
	ID        string    `json:"id"`
	Status    string    `json:"status" jsonschema:"candidate, ai_pick, selected or rejected"`
	ModelName string    `json:"model_name"`
	Adopted   bool      `json:"adopted" jsonschema:"Shot が使っている Take / the Take the Shot uses"`
	HasVideo  bool      `json:"has_video" jsonschema:"false when the file was archived or is not made yet"`
	CreatedAt time.Time `json:"created_at"`
}

// Storyboard is the storyboard frame of a Shot.
type Storyboard struct {
	Description string `json:"description"`
	Framing     string `json:"framing,omitempty"`
	Camera      string `json:"camera,omitempty"`
	Composition string `json:"composition,omitempty"`
	Action      string `json:"action,omitempty"`
	Dialogue    string `json:"dialogue,omitempty"`
}

// Card is a Shot with what its card shows: the Takes and which of the buttons Judge / Repair / Generate
// are accepted now.
type Card struct {
	ID              string      `json:"id"`
	SceneID         string      `json:"scene_id"`
	ProjectID       string      `json:"project_id,omitempty"`
	OrderIndex      int         `json:"order_index"`
	Objective       string      `json:"objective"`
	DurationSeconds float64     `json:"duration_seconds"`
	State           string      `json:"state" jsonschema:"PLANNING, GENERATING, JUDGING, REPAIRING, EDITING ..."`
	SelectedTakeID  string      `json:"selected_take_id,omitempty"`
	Storyboard      *Storyboard `json:"storyboard,omitempty"`
	Takes           []Take      `json:"takes"`
	JudgeableTakes  []string    `json:"judgeable_take_ids" jsonschema:"今 judge_take できる（未 Judge の）Take"`
	RepairableTakes []string    `json:"repairable_take_ids" jsonschema:"今 repair_take できる Take"`
	UsesCandidates  bool        `json:"uses_candidates" jsonschema:"画面の「生成」が Prompt Candidates 生成になるか / whether the screen's Generate runs Prompt Candidate Generation"`
}

// FromView is the card of a ShotView of a Project.
func FromView(projectID string, v service.ShotView) Card {
	c := Card{
		ID: v.ID, SceneID: v.SceneID, ProjectID: projectID, OrderIndex: v.OrderIndex, Objective: v.Objective,
		DurationSeconds: v.Duration, State: string(v.State), SelectedTakeID: v.SelectedTakeID,
		Takes: make([]Take, 0, len(v.Takes)), JudgeableTakes: keys(v.Judgeable), RepairableTakes: keys(v.Repairable),
		UsesCandidates: v.Candidates,
	}
	if s := v.Storyboard; s != nil {
		c.Storyboard = &Storyboard{Description: s.Description, Framing: s.Framing, Camera: s.Camera, Composition: s.Composition, Action: s.Action, Dialogue: s.Dialogue}
	}
	for _, t := range v.Takes {
		c.Takes = append(c.Takes, takeOf(t, v.SelectedTakeID))
	}
	return c
}

// FromFragment is the card of the Shot a service action answers with.
func FromFragment(f service.ShotFragment) Card { return FromView(f.ProjectID, f.ShotView) }

func takeOf(t domain.Take, selectedID string) Take {
	return Take{ID: t.ID, Status: string(t.Status), ModelName: t.ModelName, Adopted: t.ID == selectedID, HasVideo: t.FilePath != "", CreatedAt: t.CreatedAt}
}

// keys lists the IDs of a set, sorted (never nil: the output schema wants an array).
func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for id, ok := range set {
		if ok {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}
