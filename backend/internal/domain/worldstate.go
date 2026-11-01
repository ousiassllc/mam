package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// WorldTarget names what a World State field belongs to (spec/spec.md §9.2); also the target_type of the lock table.
type WorldTarget string

// World State targets. Camera and Story exist once per Project.
const (
	TargetCharacter WorldTarget = "character"
	TargetProduct   WorldTarget = "product"
	TargetLocation  WorldTarget = "location"
	TargetCamera    WorldTarget = "camera"
	TargetStory     WorldTarget = "story"
)

// WorldTargets lists every target in display order.
func WorldTargets() []WorldTarget {
	return []WorldTarget{TargetCharacter, TargetProduct, TargetLocation, TargetCamera, TargetStory}
}

// IsSingleton reports whether the Project has exactly one of the target (no entity id).
func (t WorldTarget) IsSingleton() bool { return t == TargetCamera || t == TargetStory }

const (
	// MaxWorldValueRunes bounds one text value of the World State.
	MaxWorldValueRunes = 200
	// MaxKnownInformation bounds story.known_information.
	MaxKnownInformation = 20
)

// StoryFieldScene is story.scene, the only number of the World State: the State Extractor sets it from the Scene of the Shot.
const StoryFieldScene = "scene"

// WorldContext is the World State (spec/spec.md §9.2) as generation reads it: the Characters, Products and Locations
// with their reference assets, and the Camera and Story of the latest Shot. It is the JSON of world_state_version.state.
type WorldContext struct {
	Characters []WorldCharacter `json:"characters"`
	Products   []WorldProduct   `json:"products"`
	Locations  []WorldLocation  `json:"locations"`
	Camera     WorldCamera      `json:"camera"`
	Story      WorldStory       `json:"story"`
	// Version is the number of the latest world_state_version; 0 when no Shot has completed yet.
	Version int `json:"-"`
}

// WorldCharacter is a row of character. Emotion, Position and CurrentAction are the parts the
// State Extractor updates after each Shot.
type WorldCharacter struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Hairstyle            string `json:"hairstyle,omitempty"`
	Wardrobe             string `json:"wardrobe,omitempty"`
	Voice                string `json:"voice,omitempty"`
	Personality          string `json:"personality,omitempty"`
	Emotion              string `json:"emotion,omitempty"`
	Position             string `json:"position,omitempty"`
	CurrentAction        string `json:"current_action,omitempty"`
	FaceReferenceAssetID string `json:"face_reference,omitempty"`
	BodyReferenceAssetID string `json:"body_reference,omitempty"`
	// SeriesFaceReferenceAssetID is the face_reference of the Series canon row this Character was copied from ("" outside a
	// Series): the Face Identity Similarity is measured against it, so the face stays the same across Episodes (F-29). Not in the snapshot.
	SeriesFaceReferenceAssetID string `json:"-"`
	// VoiceReference is the reference voice a local voice provider clones (F-20, F-29); nil = none. The canon row keeps
	// one; an Episode's copy and a Branch's copy share the file.
	VoiceReference *voiceref.Reference `json:"voice_reference,omitempty"`
}

// WorldProduct is a row of product.
type WorldProduct struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	Material                 string `json:"material,omitempty"`
	Orientation              string `json:"orientation,omitempty"`
	Condition                string `json:"condition,omitempty"`
	GeometryReferenceAssetID string `json:"geometry_reference,omitempty"`
	LabelReferenceAssetID    string `json:"label_reference,omitempty"`
}

// WorldLocation is a row of location.
type WorldLocation struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Time                   string `json:"time,omitempty"`
	LightingState          string `json:"lighting_state,omitempty"`
	Weather                string `json:"weather,omitempty"`
	LayoutReferenceAssetID string `json:"layout_reference,omitempty"`
}

// WorldCamera is the camera of the latest Shot.
type WorldCamera struct {
	Lens     string `json:"lens,omitempty"`
	Position string `json:"position,omitempty"`
	Angle    string `json:"angle,omitempty"`
	Movement string `json:"movement,omitempty"`
}

// WorldStory is where the story stands after the latest Shot. Scene is 1-based (0 = unknown).
type WorldStory struct {
	Scene            int      `json:"scene,omitempty"`
	CharacterGoal    string   `json:"character_goal,omitempty"`
	KnownInformation []string `json:"known_information,omitempty"`
	PreviousEvent    string   `json:"previous_event,omitempty"`
}

// StateChange is one field of the World State that a completed Shot changed. ID is the id of the
// Character/Product/Location ("" for the singleton Camera and Story). From is absent when the
// field was empty.
type StateChange struct {
	Target WorldTarget     `json:"target"`
	ID     string          `json:"id,omitempty"`
	Field  string          `json:"field"`
	From   json.RawMessage `json:"from,omitempty"`
	To     json.RawMessage `json:"to"`
}

// StateDiff is what the State Extractor found in a completed Shot (state_diff.diff).
type StateDiff struct {
	Changes []StateChange `json:"changes"`
}

// Character returns the Character that ref names, by ID or (ignoring case) by name: the Shot Spec
// and the Reference Pack name entities either way.
func (w WorldContext) Character(ref string) (WorldCharacter, bool) {
	i := slices.IndexFunc(w.Characters, func(c WorldCharacter) bool { return sameEntity(ref, c.ID, c.Name) })
	if i < 0 {
		return WorldCharacter{}, false
	}
	return w.Characters[i], true
}

// Product returns the Product that ref names, by ID or name.
func (w WorldContext) Product(ref string) (WorldProduct, bool) {
	i := slices.IndexFunc(w.Products, func(p WorldProduct) bool { return sameEntity(ref, p.ID, p.Name) })
	if i < 0 {
		return WorldProduct{}, false
	}
	return w.Products[i], true
}

// Location returns the Location that ref names, by ID or name.
func (w WorldContext) Location(ref string) (WorldLocation, bool) {
	i := slices.IndexFunc(w.Locations, func(l WorldLocation) bool { return sameEntity(ref, l.ID, l.Name) })
	if i < 0 {
		return WorldLocation{}, false
	}
	return w.Locations[i], true
}

func sameEntity(ref, id, name string) bool {
	ref = strings.TrimSpace(ref)
	return ref != "" && (ref == id || strings.EqualFold(ref, strings.TrimSpace(name)))
}

// IsExtractable reports whether a State Diff may change the field (story.scene included).
func IsExtractable(t WorldTarget, field string) bool {
	if t == TargetStory && field == StoryFieldScene {
		return true
	}
	return slices.Contains(extractableFields[t], field)
}

// ValidateFieldValue checks the JSON value of a World State field: a text of at most
// MaxWorldValueRunes runes, a list of texts for story.known_information, a positive number for
// story.scene.
func ValidateFieldValue(t WorldTarget, field string, v json.RawMessage) error {
	switch {
	case t == TargetStory && field == StoryFieldScene:
		var n int
		if err := json.Unmarshal(v, &n); err != nil || n < 1 {
			return fmt.Errorf("%s.%s must be a positive integer", t, field)
		}
	case t == TargetStory && field == "known_information":
		var items []string
		if err := json.Unmarshal(v, &items); err != nil {
			return fmt.Errorf("%s.%s must be a list of texts", t, field)
		}
		if len(items) > MaxKnownInformation {
			return fmt.Errorf("%s.%s has more than %d items", t, field, MaxKnownInformation)
		}
		for _, s := range items {
			if s == "" || utf8.RuneCountInString(s) > MaxWorldValueRunes {
				return fmt.Errorf("%s.%s has an empty or too long item", t, field)
			}
		}
	default:
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return fmt.Errorf("%s.%s must be a text", t, field)
		}
		if utf8.RuneCountInString(s) > MaxWorldValueRunes {
			return fmt.Errorf("%s.%s is longer than %d characters", t, field, MaxWorldValueRunes)
		}
	}
	return nil
}

// Current returns the value of a field and whether the target exists in the World State (a
// missing id is false). An empty field is nil.
func (w WorldContext) Current(t WorldTarget, id, field string) (json.RawMessage, bool) {
	var v any
	switch t {
	case TargetCharacter:
		i := slices.IndexFunc(w.Characters, func(c WorldCharacter) bool { return c.ID == id })
		if i < 0 {
			return nil, false
		}
		v = w.Characters[i]
	case TargetProduct:
		i := slices.IndexFunc(w.Products, func(p WorldProduct) bool { return p.ID == id })
		if i < 0 {
			return nil, false
		}
		v = w.Products[i]
	case TargetLocation:
		i := slices.IndexFunc(w.Locations, func(l WorldLocation) bool { return l.ID == id })
		if i < 0 {
			return nil, false
		}
		v = w.Locations[i]
	case TargetCamera:
		v = w.Camera
	case TargetStory:
		v = w.Story
	default:
		return nil, false
	}
	return jsonFields(v)[field], true
}

// SetSingleton writes a field of the Camera or Story. Nil or an empty value clears the field.
func (w *WorldContext) SetSingleton(t WorldTarget, field string, v json.RawMessage) error {
	switch t {
	case TargetCamera:
		return setJSONField(&w.Camera, field, v)
	case TargetStory:
		return setJSONField(&w.Story, field, v)
	}
	return fmt.Errorf("%s is not a singleton", t)
}

// SameValue reports whether two JSON field values are the same; an absent value, "", "null" and
// [] are all "empty".
func SameValue(a, b json.RawMessage) bool { return reflect.DeepEqual(normalized(a), normalized(b)) }

func normalized(raw json.RawMessage) any {
	var v any
	if len(bytes.TrimSpace(raw)) == 0 || json.Unmarshal(raw, &v) != nil {
		return nil
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
	case []any:
		if len(x) == 0 {
			return nil
		}
	}
	return v
}

func jsonFields(v any) map[string]json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	return m
}

func setJSONField(dst any, field string, v json.RawMessage) error {
	m := jsonFields(dst)
	if normalized(v) == nil {
		delete(m, field)
	} else {
		m[field] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	reflect.ValueOf(dst).Elem().SetZero()
	return json.Unmarshal(b, dst)
}

// Text renders the World State for the prompt of an Agent, one line per Character, Product and
// Location (with the ids the Agent must use) and one each for the Camera and Story once set.
func (w WorldContext) Text() string {
	var b strings.Builder
	for _, c := range w.Characters {
		fmt.Fprintf(&b, "- character id=%q name=%q hairstyle=%q wardrobe=%q personality=%q voice=%q emotion=%q position=%q current_action=%q\n",
			c.ID, c.Name, c.Hairstyle, c.Wardrobe, c.Personality, c.Voice, c.Emotion, c.Position, c.CurrentAction)
	}
	for _, p := range w.Products {
		fmt.Fprintf(&b, "- product id=%q name=%q material=%q orientation=%q condition=%q\n", p.ID, p.Name, p.Material, p.Orientation, p.Condition)
	}
	for _, l := range w.Locations {
		fmt.Fprintf(&b, "- location id=%q name=%q time=%q lighting_state=%q weather=%q\n", l.ID, l.Name, l.Time, l.LightingState, l.Weather)
	}
	if c := w.Camera; c != (WorldCamera{}) {
		fmt.Fprintf(&b, "- camera lens=%q position=%q angle=%q movement=%q\n", c.Lens, c.Position, c.Angle, c.Movement)
	}
	if s := w.Story; s.Scene != 0 || s.CharacterGoal != "" || len(s.KnownInformation) > 0 || s.PreviousEvent != "" {
		known, _ := json.Marshal(s.KnownInformation)
		if s.KnownInformation == nil {
			known = []byte("[]")
		}
		fmt.Fprintf(&b, "- story scene=%d character_goal=%q known_information=%s previous_event=%q\n", s.Scene, s.CharacterGoal, known, s.PreviousEvent)
	}
	return b.String()
}
