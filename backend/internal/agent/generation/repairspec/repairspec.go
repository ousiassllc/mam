// Package repairspec holds the pure parts of a repair of a Shot Spec by the Repair Agent (spec/spec.md §13):
// the direction the Prompt Planner receives, the parts of the spec that stay fixed, and Reference Reinforcement.
package repairspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Repair is what the Judge and the Repair Agent say about the Take that is repaired.
type Repair struct {
	// Issues are the Issues of the Judge result being repaired.
	Issues []domain.JudgeIssue
	// Instructions are snake_case repair instructions ("reduce_camera_speed_40_percent").
	Instructions []string
	// Preserve is what the Judge found good and must be kept ("character", "acting", ...).
	Preserve []string
	// Ranges are the time ranges of the Take that are being fixed (Partial Regeneration).
	Ranges []domain.TimeRange
	// Direction is a free-text direction of the director (English).
	Direction string
}

// Instruction is the direction the Prompt Planner receives: the Issues in their structured
// form and the repair instructions, in English.
func Instruction(r Repair) string {
	var b strings.Builder
	if len(r.Issues) == 0 {
		b.WriteString("The director wants a new take of this shot with a change.")
	} else {
		b.WriteString("The previous take of this shot failed the video judge. Revise the spec so the next take fixes these issues:")
	}
	for _, is := range r.Issues {
		fmt.Fprintf(&b, "\n- %s (%s)", is.Type, is.Severity)
		if is.TimeRange != "" {
			fmt.Fprintf(&b, " at %s", is.TimeRange)
		}
		if is.Description != "" {
			fmt.Fprintf(&b, ": %s", is.Description)
		}
	}
	if r.Direction != "" {
		b.WriteString("\nDirector's instruction: " + r.Direction)
	}
	if len(r.Ranges) > 0 {
		texts := make([]string, len(r.Ranges))
		for i, tr := range r.Ranges {
			texts[i] = fmt.Sprintf("%.1f-%.1fs", tr.From, tr.To)
		}
		b.WriteString("\nOnly " + strings.Join(texts, ", ") + " of the new take will be used, spliced into the previous take: change only what happens there and keep everything else as it was.")
	}
	if len(r.Instructions) > 0 {
		b.WriteString("\nRepair instructions: " + strings.Join(r.Instructions, ", ") + ".")
	}
	if len(r.Preserve) > 0 {
		b.WriteString("\nKeep what was good: " + strings.Join(r.Preserve, ", ") + ".")
	}
	return b.String()
}

// PreservedParts maps what the Judge found good ("character", "background", ...) to the parts of
// the Shot Spec that then stay fixed. Unknown words are ignored.
func PreservedParts(preserve []string) []domain.SpecPart {
	var parts []domain.SpecPart
	for _, p := range preserve {
		var part domain.SpecPart
		switch p {
		case "character", "acting", "camera", "lighting", "product":
			part = domain.SpecPart(p)
		case "background", "environment", "location":
			part = domain.PartLocation
		default:
			continue
		}
		if !slices.Contains(parts, part) {
			parts = append(parts, part)
		}
	}
	return parts
}

// BoostedKinds are the Reference kinds Reference Reinforcement strengthens: those of the Issues that
// failed (a drifting face → Character, a deformed logo → Product, ...) and of the instructions.
func BoostedKinds(r Repair) []domain.ReferenceKind {
	var kinds []domain.ReferenceKind
	add := func(k domain.ReferenceKind) {
		if !slices.Contains(kinds, k) {
			kinds = append(kinds, k)
		}
	}
	for _, is := range r.Issues {
		switch {
		case is.Type == domain.IssueTypeFaceIdentityDrift, strings.Contains(is.Type, "face"), strings.Contains(is.Type, "character"),
			is.Dimension == domain.DimCharacterIdentity, is.Dimension == domain.DimCharacterConsistency, is.Dimension == domain.DimFace:
			add(domain.RefCharacter)
		case strings.Contains(is.Type, "product"), strings.Contains(is.Type, "logo"), strings.Contains(is.Type, "label"),
			is.Dimension == domain.DimProductAccuracy, is.Dimension == domain.DimProductGeometry, is.Dimension == domain.DimLogoTextAccuracy:
			add(domain.RefProduct)
		case is.Dimension == domain.DimBackground, is.Dimension == domain.DimSceneContinuity:
			add(domain.RefLocation)
		case is.Dimension == domain.DimLighting:
			add(domain.RefLighting)
		}
	}
	for _, in := range r.Instructions {
		switch {
		case strings.Contains(in, "face"), strings.Contains(in, "character"):
			add(domain.RefCharacter)
		case strings.Contains(in, "product"), strings.Contains(in, "logo"):
			add(domain.RefProduct)
		}
	}
	if len(kinds) == 0 {
		add(domain.RefCharacter)
		add(domain.RefProduct)
	}
	return kinds
}

// Reinforce adds the constraints that make the model keep what the boosted References show.
func Reinforce(spec domain.ShotSpec, boost []domain.ReferenceKind) domain.ShotSpec {
	spec.Constraints = slices.Clone(spec.Constraints)
	add := func(c string) {
		if !slices.Contains(spec.Constraints, c) {
			spec.Constraints = append(spec.Constraints, c)
		}
	}
	for _, k := range boost {
		switch k {
		case domain.RefCharacter:
			if spec.Character != nil {
				add("preserve_face_identity")
			}
		case domain.RefProduct:
			if spec.Product != nil {
				add("preserve_product_geometry")
				add("preserve_logo")
			}
		case domain.RefLocation:
			add("preserve_environment_geometry")
		}
	}
	return spec
}

// ReinforceReferences puts the References of the boosted kinds first (the model keeps the first ones
// when it accepts fewer). Whether the Keyframe is left out is decided per model in modeladapter.Prepare
// (VideoJob.PreferReferences), so a fail over model keeps the input it can use.
func ReinforceReferences(refs []domain.Reference, boost []domain.ReferenceKind) []domain.Reference {
	out := slices.Clone(refs)
	rank := func(k domain.ReferenceKind) int {
		if i := slices.Index(boost, k); i >= 0 {
			return i
		}
		return len(boost)
	}
	slices.SortStableFunc(out, func(a, b domain.Reference) int { return rank(a.Kind) - rank(b.Kind) })
	return out
}
