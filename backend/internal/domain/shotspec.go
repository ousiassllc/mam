package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ShotSpec is the Canonical Shot Spec (spec/spec.md §7.1): the model-independent description of
// one Shot. It is stored as JSON in shot.shot_spec, next to the keys of other writers
// ("storyboard", "references"), and as a snapshot in prompt_version.shot_spec_snapshot.
//
// Every free-text value is English: the spec is what the Model Adapter turns into prompts
// (spec/requirements/non-functional.md §9). Identifiers such as "inherit_previous" are English too.
type ShotSpec struct {
	ShotObjective string         `json:"shot_objective"`
	Duration      string         `json:"duration"` // "4.0s"
	Character     *SpecActor     `json:"character,omitempty"`
	Product       *SpecProduct   `json:"product,omitempty"`
	Location      *SpecLocation  `json:"location,omitempty"`
	Acting        []ActingBeat   `json:"acting,omitempty"`
	Camera        SpecCamera     `json:"camera"`
	Lighting      SpecLighting   `json:"lighting"`
	Style         string         `json:"style,omitempty"`
	Continuity    SpecContinuity `json:"continuity"`
	Constraints   []string       `json:"constraints,omitempty"`
}

// SpecActor is the character of the Shot.
type SpecActor struct {
	ID       string `json:"id"`
	Identity string `json:"identity,omitempty"` // "locked"
	Wardrobe string `json:"wardrobe,omitempty"` // "inherit_previous" or a description
	Hair     string `json:"hair,omitempty"`
}

// SpecProduct is the product shown in the Shot.
type SpecProduct struct {
	ID          string `json:"id"`
	Geometry    string `json:"geometry,omitempty"` // "locked"
	Label       string `json:"label,omitempty"`
	Orientation string `json:"orientation,omitempty"`
}

// SpecLocation is where the Shot takes place.
type SpecLocation struct {
	ID      string `json:"id"`
	Time    string `json:"time,omitempty"`
	Weather string `json:"weather,omitempty"`
}

// ActingBeat is what the character does during a time range ("0.7-1.3", seconds).
type ActingBeat struct {
	Time   string `json:"time"`
	Action string `json:"action"`
}

// SpecCamera is the camera work.
type SpecCamera struct {
	Lens     string `json:"lens,omitempty"`
	Framing  string `json:"framing"`
	Movement string `json:"movement,omitempty"`
}

// SpecLighting is the lighting; Inherit "previous_shot" keeps the previous Shot's lighting.
type SpecLighting struct {
	Inherit      string `json:"inherit,omitempty"`
	KeyDirection string `json:"key_direction,omitempty"`
	Mood         string `json:"mood,omitempty"`
}

// SpecContinuity ties the Shot to the previous one.
type SpecContinuity struct {
	StartFromPreviousFinalState bool `json:"start_from_previous_final_state"`
}

// shotSpecKeys are the shot_spec keys owned by the Canonical Shot Spec.
var shotSpecKeys = []string{
	"shot_objective", "duration", "character", "product", "location", "acting",
	"camera", "lighting", "style", "continuity", "constraints",
}

// FormatDuration renders seconds as the spec's duration text, "4.0s".
func FormatDuration(seconds float64) string { return strconv.FormatFloat(seconds, 'f', 1, 64) + "s" }

// ParseTimeRange reads an acting time range such as "0.7-1.3" (an optional trailing "s" is ignored).
func ParseTimeRange(s string) (from, to float64, err error) {
	a, b, ok := strings.Cut(strings.TrimSuffix(strings.TrimSpace(s), "s"), "-")
	if !ok {
		return 0, 0, fmt.Errorf("time range %q is not \"from-to\"", s)
	}
	if from, err = strconv.ParseFloat(strings.TrimSpace(a), 64); err != nil {
		return 0, 0, fmt.Errorf("time range %q: %w", s, err)
	}
	if to, err = strconv.ParseFloat(strings.TrimSpace(b), 64); err != nil {
		return 0, 0, fmt.Errorf("time range %q: %w", s, err)
	}
	return from, to, nil
}

// Validate checks that the spec can drive a generation for a Shot of the given duration
// (seconds): it has an objective and framing, acting beats lie inside the Shot and in order,
// and every text is English (no Japanese/Chinese characters).
func (s ShotSpec) Validate(duration float64) error {
	if strings.TrimSpace(s.ShotObjective) == "" {
		return fmt.Errorf("shot_objective is empty")
	}
	if strings.TrimSpace(s.Camera.Framing) == "" {
		return fmt.Errorf("camera.framing is empty")
	}
	end := 0.0
	for i, b := range s.Acting {
		from, to, err := ParseTimeRange(b.Time)
		if err != nil {
			return fmt.Errorf("acting[%d]: %w", i, err)
		}
		if from < end-0.05 || to <= from || to > duration+0.05 {
			return fmt.Errorf("acting[%d]: time %q must be inside 0-%.1fs and follow the previous beat", i, b.Time, duration)
		}
		if strings.TrimSpace(b.Action) == "" {
			return fmt.Errorf("acting[%d]: action is empty", i)
		}
		end = to
	}
	for _, c := range s.Constraints {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("constraints contains an empty entry")
		}
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if hasCJK(string(raw)) {
		return fmt.Errorf("the spec must be written in English (Japanese or Chinese characters found)")
	}
	return nil
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

// CanonicalSpec returns the Canonical Shot Spec stored in the Shot and whether it has one.
func (s Shot) CanonicalSpec() (ShotSpec, bool) {
	var probe struct {
		Objective *string `json:"shot_objective"`
	}
	if err := json.Unmarshal(s.ShotSpec, &probe); err != nil || probe.Objective == nil {
		return ShotSpec{}, false
	}
	var spec ShotSpec
	if err := json.Unmarshal(s.ShotSpec, &spec); err != nil {
		return ShotSpec{}, false
	}
	return spec, true
}

// MergeShotSpec writes the Canonical Shot Spec into raw (the current shot_spec JSON): the
// spec's keys are replaced, every other key ("storyboard", "references", ...) is kept.
func MergeShotSpec(raw json.RawMessage, spec ShotSpec) (json.RawMessage, error) {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var fresh map[string]json.RawMessage
	if err := json.Unmarshal(specJSON, &fresh); err != nil {
		return nil, err
	}
	doc, err := decodeSpecMap(raw)
	if err != nil {
		return nil, err
	}
	for _, k := range shotSpecKeys {
		delete(doc, k)
	}
	for k, v := range fresh {
		doc[k] = v
	}
	return json.Marshal(doc)
}

func decodeSpecMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	doc := map[string]json.RawMessage{}
	if len(raw) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("shot_spec is not a JSON object: %w", err)
	}
	return doc, nil
}

// ReferenceKind is a part of the Reference Pack (spec/spec.md §10).
type ReferenceKind string

// Reference kinds in the order in which they are handed to a video model when it accepts
// only some of them: identity first, the Keyframe (start frame) last as it is used separately.
const (
	RefCharacter ReferenceKind = "character"
	RefProduct   ReferenceKind = "product"
	RefLocation  ReferenceKind = "location"
	RefStyle     ReferenceKind = "style"
	RefLighting  ReferenceKind = "lighting"
	RefKeyframe  ReferenceKind = "keyframe"
)

// ReferenceKinds lists every kind in priority order.
func ReferenceKinds() []ReferenceKind {
	return []ReferenceKind{RefCharacter, RefProduct, RefLocation, RefStyle, RefLighting, RefKeyframe}
}

// Valid reports whether k is a known kind.
func (k ReferenceKind) Valid() bool {
	for _, v := range ReferenceKinds() {
		if v == k {
			return true
		}
	}
	return false
}

// Reference is one image of a Shot's Reference Pack. It is stored in the "references" key of
// shot.shot_spec. AssetID is the asset row holding the image; Prompt is the English prompt it
// was made from and Model the image model that made it.
type Reference struct {
	Kind    ReferenceKind `json:"kind"`
	Name    string        `json:"name,omitempty"` // e.g. the character or product name
	AssetID string        `json:"asset_id"`
	Prompt  string        `json:"prompt"`
	Model   ModelName     `json:"model"`
}

// References returns the Reference Pack stored in the Shot, in priority order of kind.
func (s Shot) References() []Reference {
	var doc struct {
		References []Reference `json:"references"`
	}
	if err := json.Unmarshal(s.ShotSpec, &doc); err != nil {
		return nil
	}
	refs := doc.References
	out := make([]Reference, 0, len(refs))
	for _, k := range ReferenceKinds() {
		for _, r := range refs {
			if r.Kind == k {
				out = append(out, r)
			}
		}
	}
	return out
}

// WithReferences replaces the Reference Pack in raw (the current shot_spec JSON).
func WithReferences(raw json.RawMessage, refs []Reference) (json.RawMessage, error) {
	doc, err := decodeSpecMap(raw)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		delete(doc, "references")
	} else {
		b, err := json.Marshal(refs)
		if err != nil {
			return nil, err
		}
		doc["references"] = b
	}
	return json.Marshal(doc)
}
