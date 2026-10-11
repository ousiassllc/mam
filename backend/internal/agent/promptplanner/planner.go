// Package promptplanner is the Prompt Planner (spec/spec.md §7): it turns the short natural
// language instruction of a Shot into the Canonical Shot Spec, the model-independent structured
// description that the Model Adapter later renders as the prompt of a video model. The spec is
// written in English (spec/requirements/non-functional.md §9).
package promptplanner

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Planner is the Prompt Planner Agent.
type Planner struct{ llm llm.Completer }

// New returns a Planner that reasons through c.
func New(c llm.Completer) *Planner { return &Planner{llm: c} }

// Input is what the Prompt Planner works from (spec/components/overview.md §1).
type Input struct {
	Brief    domain.Brief
	Template domain.ProductionTemplate
	Scene    domain.Scene
	Shot     domain.Shot
	// Instruction is the user's natural language instruction: what the Shot should show, and any
	// extra direction given when generating ("もう少しカメラを寄せて"). May be empty.
	Instruction string
	World       domain.WorldContext
	// Previous is the Canonical Shot Spec of the Shot before this one in story order (nil for the
	// first Shot); it carries wardrobe, lighting and the final state.
	Previous *domain.ShotSpec
	// Current is the spec the Shot already has; set when the user asks to revise it.
	Current *domain.ShotSpec
	// References lists the kinds of the Reference Pack the Shot has, so the spec relies on them.
	References []domain.Reference
	// Strategy is the shooting strategy of a Prompt Candidate (spec/spec.md §8). Empty plans the
	// Shot without a particular priority.
	Strategy domain.CandidateStrategy
	// Fixed are the parts of Current that MUST stay as they are: the LOCKed parameters and what the
	// director asked to preserve. Plan copies them from Current whatever the model answered.
	Fixed []domain.SpecPart
	// Manual are the parts of Current the user specified (MANUAL): kept unless the instruction changes them,
	// and a shooting Strategy never changes them.
	Manual []domain.SpecPart
	// Brand are the Safety / Brand Constraints of the Project (F-23). The spec MUST follow them; a
	// spec that contains a forbidden expression of a LOCK constraint is refused with
	// domain.ErrBrandViolation.
	Brand domain.BrandConstraints
	// Notes are the production notes the Planner may use as context (spec/spec.md §9.4). The caller
	// records their versions in prompt_version.note_refs.
	Notes []domain.NoteView
}

// strategyDirectives tell the Planner what a Prompt Candidate puts first. Every strategy keeps the
// constraints and the locked identity: only the priority changes.
var strategyDirectives = map[domain.CandidateStrategy]string{
	domain.CandidateStabilityFirst: "Stability First: the priority is a clean, stable clip. Keep identity, product shape and environment rock steady. " +
		"Use a static or very slow camera, one simple action per beat and conservative framing (medium or wider unless the storyboard needs closer). " +
		"Avoid fast motion, occlusion and anything that risks morphing or drift.",
	domain.CandidateActingFirst: "Acting First: the priority is the performance. Make the emotion and the acting beats the focus: expressive but physically plausible actions, " +
		"clear reactions, a closer framing (medium close-up or close-up) so the face reads. Keep the camera restrained so it does not compete with the acting.",
	domain.CandidateDynamicCamera: "Dynamic Camera: the priority is cinematic camera work. Give the shot one bold but continuous camera move that serves the story " +
		"(push-in, orbit, tracking, crane). Keep the acting readable and simple so the move carries the shot.",
}

const system = `You are the Prompt Planner of an AI video production team.
Convert the instruction for ONE shot into a Canonical Shot Spec: a structured, model-independent description that a video generation model can follow.

Return ONLY one JSON object with this shape (omit keys that do not apply):
{
  "shot_objective": "one English sentence: what happens in the shot",
  "duration": "4.0s",
  "character": {"id": "Character_A", "identity": "locked", "wardrobe": "inherit_previous", "hair": "inherit_previous"},
  "product": {"id": "product name", "geometry": "locked", "label": "text printed on the label", "orientation": "how it faces the camera"},
  "location": {"id": "place name", "time": "time of day", "weather": "weather"},
  "acting": [{"time": "0.0-0.7", "action": "subtle reaction"}],
  "camera": {"lens": "35mm", "framing": "medium", "movement": "slow_push_in"},
  "lighting": {"inherit": "previous_shot", "key_direction": "left", "mood": "soft warm"},
  "style": "visual style in a few words",
  "continuity": {"start_from_previous_final_state": true},
  "constraints": ["preserve_face_identity"]
}

Rules:
- Write every value in English, even when the input is Japanese. Translate names of people, products and places into romaji or their English name; keep text that must appear on screen (a product label) exactly as given.
- "duration" is exactly the shot duration given in the input.
- "acting" beats are in seconds from the start of the shot, in order, without gaps or overlaps, and end at the shot duration. Describe only what is visible: one action per beat.
- "framing" is one of: extreme close-up, close-up, medium close-up, medium, medium wide, wide, extreme wide.
- Use "inherit_previous" for wardrobe and hair, and "previous_shot" for lighting, when the previous shot's look continues. Never invent a look that contradicts the world state or the previous shot.
- Use the ids of the characters, products and locations of the world state when the shot uses them.
- The world state is the state left by the previous shot (positions, emotions, current actions, wardrobe, camera, story). Start this shot from it and keep it consistent unless the instruction changes it.
- "constraints" are short snake_case rules the generation must keep (preserve_face_identity, preserve_wardrobe, preserve_product_geometry, preserve_logo, do_not_add_characters ...). Add the ones that protect what the references show.
- Do not invent facts about the product (effects, prices, awards).`

// Plan returns the Canonical Shot Spec of the Shot. The spec always has the Shot's duration; an
// answer that is not a usable spec (no objective or framing, acting outside the Shot, text that is
// not English) is an error wrapping planning.ErrInvalidPlan and is never returned.
func (p *Planner) Plan(ctx context.Context, in Input) (domain.ShotSpec, error) {
	spec, err := llm.CompleteJSON[domain.ShotSpec](ctx, p.llm, llm.AgentPromptPlanner, llm.Request{System: system, User: userPrompt(in)})
	if err != nil {
		return domain.ShotSpec{}, fmt.Errorf("prompt planner: %w", err)
	}
	spec.Duration = domain.FormatDuration(in.Shot.Duration)
	if in.Current != nil {
		spec = domain.PreserveParts(spec, *in.Current, in.Fixed)
	}
	if err := spec.Validate(in.Shot.Duration); err != nil {
		return domain.ShotSpec{}, fmt.Errorf("prompt planner: %w", planning.Invalidf("%v", err))
	}
	if err := in.Brand.CheckText(spec.BrandText()); err != nil {
		return domain.ShotSpec{}, fmt.Errorf("prompt planner: the spec breaks a brand constraint: %w", err)
	}
	return spec, nil
}

func userPrompt(in Input) string {
	var b strings.Builder
	b.WriteString("## Creative Brief\n" + planning.BriefText(in.Brief, in.Template))
	fmt.Fprintf(&b, "\n## Scene %d: %s\nLocation: %s / Time: %s\nObjective: %s\n", in.Scene.OrderIndex+1, in.Scene.Title, in.Scene.Location, in.Scene.Time, in.Scene.Objective)
	fmt.Fprintf(&b, "\n## Shot %d\nObjective: %s\nDuration: %s\n", in.Shot.OrderIndex+1, in.Shot.Objective, domain.FormatDuration(in.Shot.Duration))
	if f, ok := in.Shot.Storyboard(); ok {
		b.WriteString("Storyboard frame:\n")
		for _, r := range [][2]string{{"description", f.Description}, {"framing", f.Framing}, {"camera", f.Camera}, {"composition", f.Composition}, {"action", f.Action}, {"dialogue", f.Dialogue}} {
			if r[1] != "" {
				fmt.Fprintf(&b, "- %s: %s\n", r[0], r[1])
			}
		}
	}
	if s := strings.TrimSpace(in.Instruction); s != "" {
		b.WriteString("\n## Instruction from the director (must be reflected)\n" + s + "\n")
	}
	if d, ok := strategyDirectives[in.Strategy]; ok {
		b.WriteString("\n## Shooting strategy of this candidate (must be reflected)\n" + d + "\n")
		if in.Current != nil && len(in.Manual) > 0 {
			b.WriteString("The strategy applies to everything except the parts the user specified himself (" + partNames(in.Manual) + "): keep those as they are.\n")
		}
	}
	if w := in.World.Text(); w != "" {
		b.WriteString("\n## World state\n" + w)
	}
	if in.Previous != nil {
		b.WriteString("\n## Previous shot spec\n" + jsonText(*in.Previous) + "\n")
	}
	if in.Current != nil {
		b.WriteString("\n## Current spec of this shot (revise it according to the instruction; keep what the instruction does not change)\n" + jsonText(*in.Current) + "\n")
	}
	if in.Current != nil && len(in.Fixed) > 0 {
		b.WriteString("\n## Fixed parts (LOCKED or to be preserved: copy them from the current spec unchanged)\n" + partNames(in.Fixed) + "\n")
	}
	if in.Current != nil && len(in.Manual) > 0 {
		b.WriteString("\n## Parts the user specified himself (keep them unless the instruction explicitly changes them)\n" + partNames(in.Manual) + "\n")
	}
	if section := in.Brand.PromptSection(); section != "" {
		b.WriteString("\n## Brand / Safety constraints (the spec MUST follow every LOCKED one; follow ADVISORY ones where possible)\n" + section)
	}
	if len(in.References) > 0 {
		b.WriteString("\n## Reference images available\n")
		for _, r := range in.References {
			fmt.Fprintf(&b, "- %s%s\n", r.Kind, nameSuffix(r.Name))
		}
	}
	if t := domain.NotesText(in.Notes); t != "" {
		b.WriteString("\n## Production notes (context only: the world state, the constraints and the director's instruction win over them)\n" + t)
	}
	return b.String()
}

func nameSuffix(n string) string {
	if n == "" {
		return ""
	}
	return ": " + n
}

func partNames(parts []domain.SpecPart) string {
	names := make([]string, len(parts))
	for i, p := range parts {
		names[i] = string(p)
	}
	return strings.Join(names, ", ")
}
