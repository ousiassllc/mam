// Package stateextractor is the State Extractor (spec/spec.md §9.3): after a Shot is completed it
// finds what the Shot changed in the World State and returns it as a State Diff. Fields the user
// has LOCKed are never part of the diff (F-10).
package stateextractor

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Extractor is the State Extractor Agent.
type Extractor struct{ llm llm.Completer }

// New returns an Extractor that reasons through c.
func New(c llm.Completer) *Extractor { return &Extractor{llm: c} }

// Input is what the State Extractor works from (spec/components/overview.md §1).
type Input struct {
	Scene domain.Scene
	Shot  domain.Shot
	// Spec is the Canonical Shot Spec the completed Shot was generated from.
	Spec  domain.ShotSpec
	World domain.WorldContext
	Locks domain.LockSet
}

// llmTargets are the targets the model may change, in prompt order. story.scene is set from the
// Scene of the Shot, not by the model.
var llmTargets = []domain.WorldTarget{domain.TargetCharacter, domain.TargetProduct, domain.TargetCamera, domain.TargetStory}

func llmFields(t domain.WorldTarget) []string {
	var out []string
	for _, f := range domain.ExtractableFields(t) {
		if f != domain.StoryFieldScene {
			out = append(out, f)
		}
	}
	return out
}

func systemPrompt() string {
	var fields strings.Builder
	for _, t := range llmTargets {
		fmt.Fprintf(&fields, "- %s: %s\n", t, strings.Join(llmFields(t), ", "))
	}
	return `You are the State Extractor of an AI video production team.
A shot has just been completed. Compare the state at the END of the shot with the current world state and return only the fields that changed.

Return ONLY one JSON object with this shape:
{"changes": [
  {"target": "character", "id": "<id from the world state>", "field": "emotion", "value": "relieved"},
  {"target": "product", "id": "<id from the world state>", "field": "condition", "value": "opened"},
  {"target": "camera", "field": "movement", "value": "static"},
  {"target": "story", "field": "known_information", "value": ["heard_noise_outside", "found_key"]}
]}

Fields you may change:
` + fields.String() + `
Rules:
- List a field only when its value at the end of the shot differs from the current world state. Return {"changes": []} when nothing changed.
- "character" and "product" changes need the "id" of an entry of the world state; never invent characters or products. "camera" and "story" have no "id".
- "position" is where the character or camera is at the last frame; "current_action" is what the character is doing at the last frame; "emotion" is how the character feels at the end.
- "known_information" is the COMPLETE list after the shot: keep the current items and add what the shot reveals. Every other value is one short text.
- Write every value in English, short snake_case or a few words, in the style of the current values. Do not describe anything the shot does not show or clearly imply.`
}

// Extract returns the State Diff of the completed Shot: the fields whose value at the end of the
// Shot differs from the World State. Answers that name an unknown entity or field, a value of the
// wrong shape, an unchanged value or a LOCKed field are left out, so every change of the diff is
// applicable. story.scene follows the Scene of the Shot.
func (e *Extractor) Extract(ctx context.Context, in Input) (domain.StateDiff, error) {
	type change struct {
		Target domain.WorldTarget `json:"target"`
		ID     string             `json:"id"`
		Field  string             `json:"field"`
		Value  json.RawMessage    `json:"value"`
	}
	answer, err := llm.CompleteJSON[struct {
		Changes []change `json:"changes"`
	}](ctx, e.llm, llm.AgentStateExtractor, llm.Request{System: systemPrompt(), User: userPrompt(in)})
	if err != nil {
		return domain.StateDiff{}, fmt.Errorf("state extractor: %w", err)
	}

	diff := domain.StateDiff{Changes: []domain.StateChange{}}
	seen := map[domain.LockKey]bool{}
	add := func(t domain.WorldTarget, id, field string, to json.RawMessage) {
		key := domain.LockKey{Target: t, ID: id, Field: field}
		if t.IsSingleton() {
			key.ID, id = "", ""
		}
		if seen[key] || domain.SameValue(to, nil) || !domain.IsExtractable(t, field) || in.Locks.Locked(t, id, field) ||
			domain.ValidateFieldValue(t, field, to) != nil {
			return
		}
		from, exists := in.World.Current(t, id, field)
		if !exists || domain.SameValue(from, to) {
			return
		}
		seen[key] = true
		diff.Changes = append(diff.Changes, domain.StateChange{Target: t, ID: id, Field: field, From: from, To: to})
	}

	add(domain.TargetStory, "", domain.StoryFieldScene, json.RawMessage(strconv.Itoa(in.Scene.OrderIndex+1)))
	for _, c := range answer.Changes {
		if c.Field == domain.StoryFieldScene {
			continue
		}
		add(c.Target, c.ID, c.Field, c.Value)
	}
	return diff, nil
}

func userPrompt(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Scene %d: %s\nLocation: %s / Time: %s\nObjective: %s\n", in.Scene.OrderIndex+1, in.Scene.Title, in.Scene.Location, in.Scene.Time, in.Scene.Objective)
	fmt.Fprintf(&b, "\n## Completed shot %d\nObjective: %s\nDuration: %s\n", in.Shot.OrderIndex+1, in.Shot.Objective, domain.FormatDuration(in.Shot.Duration))
	spec, _ := json.Marshal(in.Spec)
	b.WriteString("\n## Canonical Shot Spec of the completed shot\n" + string(spec) + "\n")
	b.WriteString("\n## Current world state (before this shot)\n")
	if w := in.World.Text(); w != "" {
		b.WriteString(w)
	} else {
		b.WriteString("(empty)\n")
	}
	return b.String()
}
