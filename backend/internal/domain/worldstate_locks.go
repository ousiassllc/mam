package domain

import "slices"

// LockKey addresses one field under a LOCK. ID is "" for Camera and Story.
type LockKey struct {
	Target WorldTarget
	ID     string
	Field  string
}

// LockSet is the set of fields held back from an editor: the ones the user has LOCKed (lock.mode = 'lock',
// spec/spec.md §6), or, for what the State Extractor may not change, also the MANUAL Story.
type LockSet map[LockKey]struct{}

// Locked reports whether the field is LOCKed. The Story parameter of spec/spec.md §6 (the lock row
// story/story, set by PUT /projects/:id/locks) freezes every field of the Story.
func (l LockSet) Locked(t WorldTarget, id, field string) bool {
	if t.IsSingleton() {
		id = ""
	}
	if _, ok := l[LockKey{t, id, field}]; ok {
		return true
	}
	if t == TargetStory {
		_, ok := l[LockKey{TargetStory, "", string(ParamStory)}]
		return ok
	}
	return false
}

var editableFields = map[WorldTarget][]string{
	TargetCharacter: {"hairstyle", "wardrobe", "voice", "personality", "emotion", "position", "current_action"},
	TargetProduct:   {"material", "orientation", "condition"},
	TargetLocation:  {"time", "lighting_state", "weather"},
}

// The State Extractor changes what a Shot can change. Personality, voice and material are set by the user only. Location
// State (time of day, lighting, weather) is updated for the Location the Shot takes place in (spec/spec.md §9.3, M2).
var extractableFields = map[WorldTarget][]string{
	TargetCharacter: {"hairstyle", "wardrobe", "emotion", "position", "current_action"},
	TargetProduct:   {"orientation", "condition"},
	TargetLocation:  {"time", "lighting_state", "weather"},
	TargetCamera:    {"lens", "position", "angle", "movement"},
	TargetStory:     {"character_goal", "known_information", "previous_event"},
}

// lockOnlyFields are the LOCKable attributes that are not text fields of the World State: the look of a Character, the shape
// and label of a Product (the reference images) and the layout of a Location. They name lock rows only (spec/architecture/er.md §4).
var lockOnlyFields = map[WorldTarget][]string{
	TargetCharacter: {LockParamIdentity},
	TargetProduct:   {LockParamGeometry, LockParamLabel},
	TargetLocation:  {LockFieldLayoutReference},
}

// LockFieldLayoutReference is the lock attribute of a Location's layout reference image.
const LockFieldLayoutReference = "layout_reference"

// EditableFields lists the fields the user may edit with PUT /projects/:id/{characters,products,locations}/:id,
// in display order. Camera and Story have none.
func EditableFields(t WorldTarget) []string { return slices.Clone(editableFields[t]) }

// ExtractableFields lists the fields the State Extractor may change, in prompt order.
func ExtractableFields(t WorldTarget) []string { return slices.Clone(extractableFields[t]) }

// IsEditable reports whether the user may edit the field.
func IsEditable(t WorldTarget, field string) bool { return slices.Contains(editableFields[t], field) }

// LockableFields lists the attributes of the target the user can switch between AUTO and LOCK, in
// display order: the editable fields, then the reference attributes (identity, geometry, label,
// layout_reference); Camera and Story have their extractable fields (and story.scene).
func LockableFields(t WorldTarget) []string {
	switch t {
	case TargetCamera:
		return ExtractableFields(t)
	case TargetStory:
		return append([]string{StoryFieldScene}, extractableFields[t]...)
	}
	return append(EditableFields(t), lockOnlyFields[t]...)
}

// IsLockable reports whether the attribute can be LOCKed.
func IsLockable(t WorldTarget, field string) bool { return slices.Contains(LockableFields(t), field) }

// ReferenceSlot is a place of the World State that holds a reference image of the Reference Library (spec/spec.md §10.1):
// the image of a Character's face or body, of a Product's shape or label, or of a Location's layout.
type ReferenceSlot struct {
	Target WorldTarget
	Name   string        // "face", "body", "geometry", "label", "layout"
	Kind   ReferenceKind // the kind of library image that fits
	Lock   string        // the lock attribute that freezes the slot
	Column string        // the column of the entity table
}

// Field is the name of the slot in the World State snapshot (world_state_version.state): "face_reference", "layout_reference", ...
func (s ReferenceSlot) Field() string { return s.Name + "_reference" }

// ReferenceSlots lists every slot.
func ReferenceSlots() []ReferenceSlot {
	return []ReferenceSlot{
		{TargetCharacter, "face", RefCharacter, LockParamIdentity, "face_reference_asset_id"},
		{TargetCharacter, "body", RefCharacter, LockParamIdentity, "body_reference_asset_id"},
		{TargetProduct, "geometry", RefProduct, LockParamGeometry, "geometry_reference_asset_id"},
		{TargetProduct, "label", RefProduct, LockParamLabel, "label_reference_asset_id"},
		{TargetLocation, "layout", RefLocation, LockFieldLayoutReference, "layout_reference_asset_id"},
	}
}

// FindReferenceSlot returns the slot of the target with the given name.
func FindReferenceSlot(t WorldTarget, name string) (ReferenceSlot, bool) {
	for _, s := range ReferenceSlots() {
		if s.Target == t && s.Name == name {
			return s, true
		}
	}
	return ReferenceSlot{}, false
}
