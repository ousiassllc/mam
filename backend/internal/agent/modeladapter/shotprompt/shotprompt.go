// Package shotprompt writes a Canonical Shot Spec as the English prose a video model is prompted with
// (spec/architecture/model-adapters.md §9.3), and fits it to a model's length limit.
package shotprompt

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/textutil"
)

// constraintPhrases are the wording of the constraints the Prompt Planner uses most.
var constraintPhrases = map[string]string{
	"preserve_face_identity":        "keep the character's face identical to the reference images",
	"preserve_wardrobe":             "keep the character's wardrobe unchanged",
	"preserve_hairstyle":            "keep the character's hairstyle unchanged",
	"preserve_environment_geometry": "keep the layout of the environment unchanged",
	"preserve_product_geometry":     "keep the product's shape exactly as in the reference images",
	"preserve_logo":                 "keep the logo and label text sharp and undistorted",
	"do_not_add_characters":         "no additional people appear",
}

// negativeTerms are what every prompt asks the model to avoid.
var negativeTerms = []string{"blurry", "distorted face", "extra fingers", "warped text", "flickering", "watermark", "subtitles"}

// Fit joins body and tail and keeps the tail whole: when the result would exceed limit, the body is
// shortened. The tail holds what the model must not lose (the Constraints, the Avoid list); the
// body, whose last sentences are the least specific, is what gives way.
func Fit(body, tail string, limit int) string {
	if n := limit - len([]rune(tail)); n > 0 {
		return textutil.TruncateRunes(body, n) + tail
	}
	return textutil.TruncateRunes(strings.TrimSpace(tail), limit)
}

// AvoidSentence is the " Avoid: ..." sentence of a prompt without a negative prompt field.
func AvoidSentence(avoid []string) string { return " Avoid: " + strings.Join(avoid, ", ") + "." }

// Describe writes the spec as prose. It returns the prompt without its Constraints, the Constraints
// sentence (empty when there is none; " Constraints: ..." with its leading space) and the things to avoid.
func Describe(s domain.ShotSpec, seconds float64, hasImages bool) (body, constraints string, avoid []string) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s.", strings.TrimRight(strings.TrimSpace(s.ShotObjective), ". "))
	if s.Style != "" {
		fmt.Fprintf(&b, " Visual style: %s.", humanize(s.Style))
	}
	if c := s.Character; c != nil {
		parts := []string{}
		if c.Wardrobe != "" && c.Wardrobe != "inherit_previous" {
			parts = append(parts, "wearing "+humanize(c.Wardrobe))
		}
		if c.Hair != "" && c.Hair != "inherit_previous" {
			parts = append(parts, "hair: "+humanize(c.Hair))
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, " The character is %s.", strings.Join(parts, ", "))
		}
	}
	if p := s.Product; p != nil && (p.Label != "" || p.Orientation != "") {
		fmt.Fprintf(&b, " The product %s is shown%s%s.", humanize(p.ID),
			ifSet(p.Orientation, ", oriented "), ifSet(p.Label, ", with the label reading "))
	}
	if l := s.Location; l != nil && (l.Time != "" || l.Weather != "") {
		fmt.Fprintf(&b, " Setting:%s%s.", ifSet(l.Time, " time of day "), ifSet(l.Weather, " weather "))
	}
	if len(s.Acting) > 0 {
		b.WriteString(" Action timeline:")
		for _, a := range s.Acting {
			fmt.Fprintf(&b, " %ss: %s;", strings.TrimSuffix(a.Time, "s"), humanize(a.Action))
		}
	}
	cam := []string{}
	if s.Camera.Framing != "" {
		cam = append(cam, humanize(s.Camera.Framing)+" shot")
	}
	if s.Camera.Lens != "" {
		cam = append(cam, s.Camera.Lens+" lens")
	}
	if s.Camera.Movement != "" {
		cam = append(cam, humanize(s.Camera.Movement))
	}
	if len(cam) > 0 {
		fmt.Fprintf(&b, " Camera: %s.", strings.Join(cam, ", "))
	}
	light := []string{}
	if s.Lighting.KeyDirection != "" {
		light = append(light, "key light from "+humanize(s.Lighting.KeyDirection))
	}
	if s.Lighting.Mood != "" {
		light = append(light, humanize(s.Lighting.Mood))
	}
	if s.Lighting.Inherit == "previous_shot" {
		light = append(light, "same lighting as the previous shot")
	}
	if len(light) > 0 {
		fmt.Fprintf(&b, " Lighting: %s.", strings.Join(light, ", "))
	}
	if s.Continuity.StartFromPreviousFinalState {
		b.WriteString(" The shot starts from the final state of the previous shot.")
	}
	fmt.Fprintf(&b, " Duration %.0f seconds, single continuous take.", seconds)
	if hasImages {
		b.WriteString(" Follow the provided reference images for identity, product shape and environment.")
	}
	var must []string
	avoid = append([]string(nil), negativeTerms...)
	for _, c := range s.Constraints {
		if term, ok := strings.CutPrefix(c, "do_not_"); ok {
			if phrase, known := constraintPhrases[c]; known {
				must = append(must, phrase)
			} else {
				must = append(must, "do not "+humanize(term))
			}
			avoid = append(avoid, humanize(term))
			continue
		}
		if phrase, ok := constraintPhrases[c]; ok {
			must = append(must, phrase)
		} else {
			must = append(must, humanize(c))
		}
	}
	if len(must) > 0 {
		constraints = fmt.Sprintf(" Constraints: %s.", strings.Join(must, "; "))
	}
	return b.String(), constraints, avoid
}

func humanize(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "_", " ")) }

func ifSet(v, prefix string) string {
	if v == "" {
		return ""
	}
	return prefix + humanize(v)
}
