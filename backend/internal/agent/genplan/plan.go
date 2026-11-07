// Package genplan is what the generation approval of the AUTO pipeline shows and compares (spec/spec.md §4.3): the plan of the paid generation
// (which Shots get a Reference Pack, which video model makes how many Takes of each) and its estimated cost. The plan has a fingerprint of its
// scope, so that a change of the Shots or of the models after the approval is told from a change of the price; the estimate says "unknown"
// for what it cannot price instead of making a number up.
package genplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// packImages is the most images a Reference Pack has: a Character, a Product, a Location, a Style, a Lighting and a Keyframe (the Art Director
// designs which of them a Shot gets while the job runs, so the estimate is this bound).
const packImages = 6

// epsilon is the tolerance of a comparison of two sums of prices.
const epsilon = 1e-9

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

// key is what of a Line the approval is about: the price is compared on its own.
type key struct {
	Pack       bool
	PackModels string
	VideoModel string
	Resolution string
	Takes      int
}

func (l Line) key() key { return key{l.Pack, l.PackModels, l.VideoModel, l.Resolution, l.Takes} }

// Plan is the plan of the generation of a Project.
type Plan struct {
	Lines []Line
	// Exists holds the ID of every Shot of the trunk, made or not: a Shot of an approval that is gone from it was deleted.
	Exists map[string]bool
	// Candidates is whether some Shot goes through Prompt Candidate Generation (the Judge picks the best Take).
	Candidates bool
	// Unpriced names the models whose price is not known.
	Unpriced []string
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

// Fingerprint identifies the scope of the plan: the Shots, whether each gets a Reference Pack and with which models, and the video model,
// resolution and number of Takes of each. The price is not in it.
func (p Plan) Fingerprint() string {
	keys := make([]struct {
		ShotID string
		Key    key
	}, len(p.Lines))
	for i, l := range p.Lines {
		keys[i].ShotID, keys[i].Key = l.ShotID, l.key()
	}
	raw, _ := json.Marshal(keys) // strings, bools and ints: Marshal cannot fail
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

// Scope is the JSON of the lines, as the approval stores them.
func (p Plan) Scope() json.RawMessage {
	raw, _ := json.Marshal(p.Lines) // Marshal cannot fail for these fields
	return raw
}

// ScopeLines reads the lines back from a stored scope.
func ScopeLines(scope json.RawMessage) ([]Line, error) {
	var lines []Line
	if err := json.Unmarshal(scope, &lines); err != nil {
		return nil, fmt.Errorf("read the approved scope: %w", err)
	}
	return lines, nil
}

// Changed says what of the plan differs from what was approved ("" when nothing does): a Shot that was added, was deleted or has other
// models or another number of Takes, or a Reference Pack that is now wanted and was not approved. A Shot that has a Take or is being made
// since is left out of the plan, and a Reference Pack that is no longer wanted (a Keyframe exists, or it was started) costs less: neither is a change.
func Changed(approved []Line, current Plan) string {
	byShot := make(map[string]Line, len(approved))
	for _, a := range approved {
		byShot[a.ShotID] = a
	}
	var added, removed, changed []int
	for _, c := range current.Lines {
		a, ok := byShot[c.ShotID]
		switch {
		case !ok:
			added = append(added, c.Number)
		case a.VideoModel != c.VideoModel || a.Resolution != c.Resolution || a.Takes != c.Takes || (c.Pack && (!a.Pack || a.PackModels != c.PackModels)):
			changed = append(changed, c.Number)
		}
	}
	for _, a := range approved {
		if !current.Exists[a.ShotID] {
			removed = append(removed, a.Number)
		}
	}
	var parts []string
	for _, p := range []struct {
		nums []int
		what string
	}{{added, "追加された Shot"}, {removed, "削除された Shot"}, {changed, "モデルや本数が変わった Shot"}} {
		if len(p.nums) > 0 {
			parts = append(parts, p.what+"（"+numbers(p.nums)+"）")
		}
	}
	return strings.Join(parts, "、")
}

// Priced is what the approval priced for the part of the plan that is still to be made: the video of each Shot that is still planned, and the Reference
// Pack of the ones that still get one. What was started since (the Reference Packs, the Shots) is not in it, so that the estimate is compared with the same part.
func Priced(approved []Line, current Plan) (c Cost) {
	byShot := make(map[string]Line, len(approved))
	for _, a := range approved {
		byShot[a.ShotID] = a
	}
	for _, l := range current.Lines {
		a, ok := byShot[l.ShotID]
		if !ok {
			continue
		}
		c = c.Add(a.VideoCost)
		if l.Pack && a.Pack {
			c = c.Add(a.PackCost)
		}
	}
	return c
}

// Increased says why the estimate cannot stand for the approved one ("" when it can): the price rose, or a price that was known no longer is
// (or the other way round), so what was agreed to is not what is asked now.
func Increased(approved, current Cost) string {
	switch {
	case current.Unknown != approved.Unknown && current.Unknown:
		return "見積が数字から「不明」に変わりました（承認時の金額を確かめられません）"
	case current.Unknown != approved.Unknown:
		return "見積が「不明」から数字に変わりました"
	case current.USD > approved.USD+epsilon:
		return fmt.Sprintf("見積が承認時の %s USD から %s USD に増えました", usd(approved.USD), usd(current.USD))
	}
	return ""
}

// Differs reports whether the estimate is not the one that was shown, in either direction: a declined plan is asked again when its price changed.
func Differs(a, b Cost) bool { return a.Unknown != b.Unknown || math.Abs(a.USD-b.USD) > epsilon }
