package stateextractor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// PromotionInput is what the State Extractor works from when a Project note gets a new version
// (spec/spec.md §9.4): the note and the World State it is compared with.
type PromotionInput struct {
	Note  domain.NoteView
	World domain.WorldContext
	Locks domain.LockSet
}

// Promotion is the proposal of a note version: the changes to the World State that can be applied,
// and the ones left out because their field is LOCKed.
type Promotion struct {
	Proposed  domain.StateDiff
	Conflicts domain.StateDiff
}

// Empty reports whether there is nothing to propose.
func (p Promotion) Empty() bool { return len(p.Proposed.Changes) == 0 }

// promotionTargets are the targets a note can be promoted to. Camera and Story describe a Shot, not
// what a memo says about a Character, Product or Location.
var promotionTargets = []domain.WorldTarget{domain.TargetCharacter, domain.TargetProduct, domain.TargetLocation}

func promotionSystem() string {
	var fields strings.Builder
	for _, t := range promotionTargets {
		fmt.Fprintf(&fields, "- %s: %s\n", t, strings.Join(domain.EditableFields(t), ", "))
	}
	return `You are the State Extractor of an AI video production team.
The director wrote a production memo. Find what in it can be written into the structured world state, and return only those fields.

Return ONLY one JSON object with this shape:
{"changes": [
  {"target": "character", "id": "<id from the world state>", "field": "hairstyle", "value": "long_black"},
  {"target": "location", "id": "<id from the world state>", "field": "time", "value": "night"}
]}

Fields you may propose:
` + fields.String() + `
Rules:
- Propose a field only when the memo states its value for an entity of the world state. Background, history, tone of voice and what must stay hidden are not fields: leave them out. Return {"changes": []} when nothing fits.
- Every change needs the "id" of an entry of the world state; never invent characters, products or locations.
- Propose a field only when its value differs from the current world state.
- Write every value in English, short snake_case or a few words, in the style of the current values.`
}

// Promote finds the structured items of a Project note version and compares them with the World
// State (spec/spec.md §9.4). Answers that name an unknown entity or field, a value of the wrong
// shape, or a value the field already has are left out; LOCKed fields go to Conflicts instead of
// Proposed, so every proposed change is applicable.
func (e *Extractor) Promote(ctx context.Context, in PromotionInput) (Promotion, error) {
	type change struct {
		Target domain.WorldTarget `json:"target"`
		ID     string             `json:"id"`
		Field  string             `json:"field"`
		Value  json.RawMessage    `json:"value"`
	}
	answer, err := llm.CompleteJSON[struct {
		Changes []change `json:"changes"`
	}](ctx, e.llm, llm.AgentStateExtractor, llm.Request{System: promotionSystem(), User: promotionPrompt(in)})
	if err != nil {
		return Promotion{}, fmt.Errorf("state extractor: %w", err)
	}
	out := Promotion{Proposed: domain.StateDiff{Changes: []domain.StateChange{}}, Conflicts: domain.StateDiff{Changes: []domain.StateChange{}}}
	seen := map[domain.LockKey]bool{}
	for _, c := range answer.Changes {
		key := domain.LockKey{Target: c.Target, ID: c.ID, Field: c.Field}
		if seen[key] || !isPromotionTarget(c.Target) || !domain.IsEditable(c.Target, c.Field) ||
			domain.SameValue(c.Value, nil) || domain.ValidateFieldValue(c.Target, c.Field, c.Value) != nil {
			continue
		}
		from, exists := in.World.Current(c.Target, c.ID, c.Field)
		if !exists || domain.SameValue(from, c.Value) {
			continue
		}
		seen[key] = true
		sc := domain.StateChange{Target: c.Target, ID: c.ID, Field: c.Field, From: from, To: c.Value}
		if in.Locks.Locked(c.Target, c.ID, c.Field) {
			out.Conflicts.Changes = append(out.Conflicts.Changes, sc)
			continue
		}
		out.Proposed.Changes = append(out.Proposed.Changes, sc)
	}
	return out, nil
}

func isPromotionTarget(t domain.WorldTarget) bool {
	for _, p := range promotionTargets {
		if p == t {
			return true
		}
	}
	return false
}

func promotionPrompt(in PromotionInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Memo: %s\n%s\n", in.Note.Version.Title, strings.TrimSpace(in.Note.Version.Content))
	b.WriteString("\n## Current world state\n")
	if w := in.World.Text(); w != "" {
		b.WriteString(w)
	} else {
		b.WriteString("(empty)\n")
	}
	return b.String()
}

const classifySystem = `You are the State Extractor of an AI video production team.
Classify a production memo into exactly one category:
- character: about a person or character (looks, personality, background, way of speaking)
- product: about the product or an object
- location: about a place or setting
- world: about the world, the story background or its rules
- tone: about the tone, mood, style or manner of the whole video
- other: anything else
Return ONLY one JSON object: {"category": "<one of the categories>"}`

// Classify returns the category of a note. An answer that is not a known category is an error
// wrapping llm.ErrInvalidOutput.
func (e *Extractor) Classify(ctx context.Context, title, content string) (domain.NoteCategory, error) {
	answer, err := llm.CompleteJSON[struct {
		Category domain.NoteCategory `json:"category"`
	}](ctx, e.llm, llm.AgentStateExtractor, llm.Request{System: classifySystem, User: fmt.Sprintf("## Memo: %s\n%s\n", title, strings.TrimSpace(content))})
	if err != nil {
		return "", fmt.Errorf("state extractor: %w", err)
	}
	if !answer.Category.Valid() {
		return "", fmt.Errorf("state extractor: %w: unknown category %q", llm.ErrInvalidOutput, answer.Category)
	}
	return answer.Category, nil
}
