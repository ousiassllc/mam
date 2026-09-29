// Package artdirector is the Art Director Agent (spec/spec.md §10): it decides which images the
// Reference Pack of a Shot needs (Character / Product / Location / Style / Lighting References and
// the Keyframe) and writes the English prompt of each. Generating the images is the job of the
// Model Adapter; the Generation Orchestrator runs both.
package artdirector

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Director is the Art Director Agent.
type Director struct{ llm llm.Completer }

// New returns a Director that reasons through c.
func New(c llm.Completer) *Director { return &Director{llm: c} }

// Input is what the Art Director works from (spec/components/overview.md §1: Storyboard output, Assets).
type Input struct {
	Brief    domain.Brief
	Template domain.ProductionTemplate
	Scene    domain.Scene
	Shot     domain.Shot
	World    domain.WorldContext
	// Spec is the Canonical Shot Spec of the Shot when it has one; the references then match it.
	Spec *domain.ShotSpec
	// UploadedImages is how many images the user uploaded (product photos, character sketches ...).
	// The Art Director says per reference whether they are its material (Plan.UseUploaded).
	UploadedImages int
}

// Plan is one image of the Reference Pack to generate.
type Plan struct {
	Kind   domain.ReferenceKind `json:"kind"`
	Name   string               `json:"name"`
	Prompt string               `json:"prompt"`
	// UseUploaded means the uploaded images are the material of this reference: the image is made
	// by editing / following them instead of from the prompt alone.
	UseUploaded bool `json:"use_uploaded,omitempty"`
}

const system = `You are the Art Director of an AI video production team.
Design the Reference Pack of ONE shot: the still images that fix the look of the shot before the video is generated.

Return ONLY one JSON object:
{"references": [
  {"kind": "character", "name": "name of the character", "prompt": "...", "use_uploaded": false},
  {"kind": "product", "name": "name of the product", "prompt": "...", "use_uploaded": true},
  {"kind": "location", "name": "name of the place", "prompt": "...", "use_uploaded": false},
  {"kind": "style", "name": "", "prompt": "..."},
  {"kind": "lighting", "name": "", "prompt": "..."},
  {"kind": "keyframe", "name": "", "prompt": "..."}
]}

Rules:
- kind is one of character, product, location, style, lighting, keyframe. Use each kind at most once.
- Include "character" only when a person or character appears, "product" only when a product appears, "location" when the shot has a setting. Always include "style", "lighting" and "keyframe".
- Every prompt is an English image-generation prompt: subject, appearance, composition, lens, lighting, background, level of detail. One paragraph, no lists. Translate names into romaji or their English name.
- character: a clean full-body or three-quarter view on a neutral background, showing face, hairstyle and wardrobe exactly as the world state and the previous references define them.
- product: the product alone from the front, sharp label and logo text exactly as given, neutral background, accurate shape and material.
- location: the empty setting, wide view, with time of day and weather.
- style: the overall visual look (palette, grain, contrast) of the video as one representative frame.
- lighting: the light of the shot (direction, softness, colour temperature) on a simple subject.
- keyframe: the first frame of the shot: characters, product and setting together, framed as the shot's camera framing says.
- "use_uploaded" is true only when the uploaded images (if any are listed in the input) show that character, product or place, so the reference must reproduce them faithfully; otherwise false.
- Do not invent facts about the product (effects, prices, awards). Do not add text to any image except a product label given in the input.`

// Design returns the images of the Reference Pack in the order kind priority. The Keyframe is always
// included; at most one image per kind. An answer that breaks these rules is an error wrapping
// planning.ErrInvalidPlan.
func (d *Director) Design(ctx context.Context, in Input) ([]Plan, error) {
	ans, err := llm.CompleteJSON[struct {
		References []Plan `json:"references"`
	}](ctx, d.llm, llm.AgentArtDirector, llm.Request{System: system, User: userPrompt(in)})
	if err != nil {
		return nil, fmt.Errorf("art director: %w", err)
	}
	seen := map[domain.ReferenceKind]bool{}
	var plans []Plan
	for i, p := range ans.References {
		p.Prompt, p.Name = strings.TrimSpace(p.Prompt), strings.TrimSpace(p.Name)
		switch {
		case !p.Kind.Valid():
			return nil, fmt.Errorf("art director: %w", planning.Invalidf("reference %d has the unknown kind %q", i+1, p.Kind))
		case seen[p.Kind]:
			return nil, fmt.Errorf("art director: %w", planning.Invalidf("kind %q appears twice", p.Kind))
		case p.Prompt == "":
			return nil, fmt.Errorf("art director: %w", planning.Invalidf("the %s reference has no prompt", p.Kind))
		case hasCJK(p.Prompt):
			return nil, fmt.Errorf("art director: %w", planning.Invalidf("the %s prompt is not English", p.Kind))
		}
		seen[p.Kind] = true
		plans = append(plans, p)
	}
	if !seen[domain.RefKeyframe] {
		return nil, fmt.Errorf("art director: %w", planning.Invalidf("the Reference Pack has no keyframe"))
	}
	ordered := make([]Plan, 0, len(plans))
	for _, k := range domain.ReferenceKinds() {
		for _, p := range plans {
			if p.Kind == k {
				ordered = append(ordered, p)
			}
		}
	}
	return ordered, nil
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

func userPrompt(in Input) string {
	var b strings.Builder
	b.WriteString("## Creative Brief\n" + planning.BriefText(in.Brief, in.Template))
	fmt.Fprintf(&b, "\n## Scene %d: %s\nLocation: %s / Time: %s\n", in.Scene.OrderIndex+1, in.Scene.Title, in.Scene.Location, in.Scene.Time)
	fmt.Fprintf(&b, "\n## Shot %d\nObjective: %s\nDuration: %s\n", in.Shot.OrderIndex+1, in.Shot.Objective, domain.FormatDuration(in.Shot.Duration))
	if f, ok := in.Shot.Storyboard(); ok {
		fmt.Fprintf(&b, "Storyboard: %s (framing: %s, camera: %s, action: %s)\n", f.Description, f.Framing, f.Camera, f.Action)
	}
	if in.Spec != nil {
		fmt.Fprintf(&b, "\nCanonical Shot Spec (English): objective=%q framing=%q lens=%q lighting mood=%q style=%q\n",
			in.Spec.ShotObjective, in.Spec.Camera.Framing, in.Spec.Camera.Lens, in.Spec.Lighting.Mood, in.Spec.Style)
	}
	if len(in.World.Characters)+len(in.World.Products)+len(in.World.Locations) > 0 {
		b.WriteString("\n## World state\n")
		for _, c := range in.World.Characters {
			fmt.Fprintf(&b, "- character %q: hairstyle=%q wardrobe=%q\n", c.Name, c.Hairstyle, c.Wardrobe)
		}
		for _, p := range in.World.Products {
			fmt.Fprintf(&b, "- product %q: material=%q orientation=%q condition=%q\n", p.Name, p.Material, p.Orientation, p.Condition)
		}
		for _, l := range in.World.Locations {
			fmt.Fprintf(&b, "- location %q: time=%q lighting=%q weather=%q\n", l.Name, l.Time, l.LightingState, l.Weather)
		}
	}
	if in.UploadedImages > 0 {
		fmt.Fprintf(&b, "\n## Uploaded images\n%d image(s) uploaded by the user (product photos, characters, places ...).\n", in.UploadedImages)
	}
	return b.String()
}
