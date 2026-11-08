// Package genplan is what the AUTO pipeline tells before it starts the paid generation (spec/spec.md §4.3): the plan of the generation (which Shots
// get a Reference Pack, which video model makes how many Takes of each) and its estimated cost. The estimate says "unknown" for what it cannot price
// instead of making a number up. A plan can be compared with the scope of a legacy approval card (Changed).
package genplan

import (
	"encoding/json"
	"fmt"
	"slices"
)

// packImages is the most images a Reference Pack has: a Character, a Product, a Location, a Style, a Lighting and a Keyframe (the Art Director
// designs which of them a Shot gets while the job runs, so the estimate is this bound).
const packImages = 6

// Cost is an estimate: the sum of the prices that are known, and whether some price is not.
type Cost struct {
	USD     float64 `json:"usd"`
	Unknown bool    `json:"unknown"`
}

// Add returns the sum of two costs; it is unknown when either is.
func (c Cost) Add(o Cost) Cost { return Cost{USD: c.USD + o.USD, Unknown: c.Unknown || o.Unknown} }

// Line is the plan of one Shot that has no Take and is not being made.
type Line struct {
	ShotID string `json:"shot_id"`
	// Number is the 1-based position of the Shot in the Project (Scene order, then Shot order), as the screens count.
	Number  int     `json:"number"`
	Seconds float64 `json:"seconds"`
	// Pack is whether the Shot gets a Reference Pack (it has no Keyframe, and none was started); PackModels says which image models make it.
	Pack       bool   `json:"pack"`
	PackModels string `json:"pack_models,omitempty"`
	VideoModel string `json:"video_model"`
	Resolution string `json:"resolution"`
	// Takes is the most Takes made of the Shot; ClipSeconds is the length of each.
	Takes       int     `json:"takes"`
	ClipSeconds float64 `json:"clip_seconds"`
	PackCost    Cost    `json:"pack_cost"`
	VideoCost   Cost    `json:"video_cost"`
}

// Plan is the plan of the generation of a Project.
type Plan struct {
	Lines []Line
	// Exists holds the ID of every Shot of the trunk, made or not: a Shot of a declined card that is gone from it was deleted.
	Exists map[string]bool
	// Candidates is whether some Shot goes through Prompt Candidate Generation (the Judge picks the best Take).
	Candidates bool
	// Unpriced names the models whose price is not known.
	Unpriced []string
	// packUnpriced is the image models of the Reference Pack whose price is not known.
	packUnpriced []string
	// PackImageModels is the image models of the Reference Pack ("" when no Shot gets one).
	PackImageModels string
	// ProjectCeiling and ShotCeiling are the budget ceilings (nil: none); ProjectSpent is what the ledger and the generations in flight hold.
	ProjectCeiling, ShotCeiling *float64
	ProjectSpent                float64
}

// Pack is the Shots that get a Reference Pack.
func (p Plan) Pack() []Line {
	var out []Line
	for _, l := range p.Lines {
		if l.Pack {
			out = append(out, l)
		}
	}
	return out
}

// PackCost is the estimate of the Reference Packs.
func (p Plan) PackCost() (c Cost) {
	for _, l := range p.Lines {
		c = c.Add(l.PackCost)
	}
	return c
}

// VideoCost is the estimate of the videos.
func (p Plan) VideoCost() (c Cost) {
	for _, l := range p.Lines {
		c = c.Add(l.VideoCost)
	}
	return c
}

// Total is the estimate of the whole plan.
func (p Plan) Total() Cost { return p.PackCost().Add(p.VideoCost()) }

// Only returns the plan of the lines keep accepts: its totals, models and unknown prices are those of these lines.
func (p Plan) Only(keep func(Line) bool) Plan {
	q := p
	q.Lines, q.PackImageModels = nil, ""
	for _, l := range p.Lines {
		if !keep(l) {
			continue
		}
		q.Lines = append(q.Lines, l)
		if l.Pack {
			q.PackImageModels = p.PackImageModels
		}
	}
	q.Unpriced = q.unpriced()
	return q
}

// unpriced names the models of the lines whose price is not known: the image models of the Reference Pack (when a Shot gets one) and the video models.
func (p Plan) unpriced() []string {
	var out []string
	if p.PackImageModels != "" {
		out = append(out, p.packUnpriced...)
	}
	for _, l := range p.Lines {
		name := "動画: " + l.VideoModel
		if l.VideoModel == "" {
			name = "動画モデルを決められない Shot"
		}
		if l.VideoCost.Unknown && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// ScopeLines reads the lines of a legacy approval card back from the scope it was stored with.
func ScopeLines(scope json.RawMessage) ([]Line, error) {
	var lines []Line
	if err := json.Unmarshal(scope, &lines); err != nil {
		return nil, fmt.Errorf("read the approved scope: %w", err)
	}
	return lines, nil
}

// Changed reports whether the plan differs from the scope of a legacy approval card that was declined: a Shot that was added, was deleted or has other
// models or another number of Takes, or a Reference Pack that is now wanted and was not on the card. A Shot that has a Take or is being made since
// is left out of the plan, and a Reference Pack that is no longer wanted (a Keyframe exists, or it was started) is not a change.
func Changed(approved []Line, current Plan) bool {
	byShot := make(map[string]Line, len(approved))
	for _, a := range approved {
		byShot[a.ShotID] = a
	}
	for _, c := range current.Lines {
		a, ok := byShot[c.ShotID]
		if !ok || a.VideoModel != c.VideoModel || a.Resolution != c.Resolution || a.Takes != c.Takes || (c.Pack && (!a.Pack || a.PackModels != c.PackModels)) {
			return true
		}
	}
	for _, a := range approved {
		if !current.Exists[a.ShotID] {
			return true
		}
	}
	return false
}
