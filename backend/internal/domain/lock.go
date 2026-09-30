package domain

import (
	"bytes"
	"encoding/json"
	"slices"
)

// LockMode is the control state of one parameter (spec/spec.md §6, F-05). It is also lock.mode.
type LockMode string

// Lock modes: AUTO lets the AI decide and change the value, MANUAL is a value the user specified
// (the AI keeps it unless told otherwise), LOCK freezes the current value against every change.
const (
	LockAuto   LockMode = "auto"
	LockManual LockMode = "manual"
	LockLocked LockMode = "lock"
)

// LockModes lists every mode in display order.
func LockModes() []LockMode { return []LockMode{LockAuto, LockManual, LockLocked} }

// Valid reports whether m is a known mode.
func (m LockMode) Valid() bool { return slices.Contains(LockModes(), m) }

// Label is the mode as the user sees it.
func (m LockMode) Label() string {
	switch m {
	case LockAuto:
		return "AUTO"
	case LockManual:
		return "MANUAL"
	case LockLocked:
		return "LOCK"
	}
	return string(m)
}

// LockParameter is a parameter of the table in spec/spec.md §6 that can be AUTO / MANUAL / LOCK.
type LockParameter string

// Parameters of a Shot (lock.target_type = "shot", target_id = the Shot) and of a Project
// (Story and Music: lock.target_type = the parameter, target_id = the Project).
const (
	ParamStory             LockParameter = "story"
	ParamMusic             LockParameter = "music"
	ParamDialogue          LockParameter = "dialogue"
	ParamCharacterIdentity LockParameter = "character_identity"
	ParamActing            LockParameter = "acting"
	ParamCamera            LockParameter = "camera"
	ParamLighting          LockParameter = "lighting"
	ParamProductGeometry   LockParameter = "product_geometry"
	ParamPrompt            LockParameter = "prompt"
	ParamVideoModel        LockParameter = "video_model"
	ParamTakeSelection     LockParameter = "take_selection"
	ParamEditing           LockParameter = "editing"
)

// LockTargetShot is the lock.target_type of the parameters that belong to one Shot.
const LockTargetShot = "shot"

// ShotLockParameters lists the parameters that PUT /shots/:id/locks accepts, in the order of §6.
func ShotLockParameters() []LockParameter {
	return []LockParameter{
		ParamDialogue, ParamCharacterIdentity, ParamActing, ParamCamera, ParamLighting,
		ParamProductGeometry, ParamPrompt, ParamVideoModel, ParamTakeSelection, ParamEditing,
	}
}

// ProjectLockParameters lists the parameters that PUT /projects/:id/locks accepts: the ones that
// do not belong to a Shot.
func ProjectLockParameters() []LockParameter { return []LockParameter{ParamStory, ParamMusic} }

// ValidForShot reports whether p is set per Shot.
func (p LockParameter) ValidForShot() bool { return slices.Contains(ShotLockParameters(), p) }

// ValidForProject reports whether p is set per Project.
func (p LockParameter) ValidForProject() bool { return slices.Contains(ProjectLockParameters(), p) }

// Label is the parameter as spec/spec.md §6 names it.
func (p LockParameter) Label() string {
	switch p {
	case ParamStory:
		return "Story"
	case ParamMusic:
		return "Music"
	case ParamDialogue:
		return "Dialogue"
	case ParamCharacterIdentity:
		return "Character Identity"
	case ParamActing:
		return "Acting"
	case ParamCamera:
		return "Camera"
	case ParamLighting:
		return "Lighting"
	case ParamProductGeometry:
		return "Product Geometry"
	case ParamPrompt:
		return "Prompt"
	case ParamVideoModel:
		return "Video Model"
	case ParamTakeSelection:
		return "Take Selection"
	case ParamEditing:
		return "Editing"
	}
	return string(p)
}

// SpecPart is a part of the Canonical Shot Spec that can be fixed while the rest is regenerated.
type SpecPart string

// Spec parts. Location is the "environment" of a Shot.
const (
	PartCharacter SpecPart = "character"
	PartProduct   SpecPart = "product"
	PartLocation  SpecPart = "location"
	PartActing    SpecPart = "acting"
	PartCamera    SpecPart = "camera"
	PartLighting  SpecPart = "lighting"
)

// SpecParts lists every part.
func SpecParts() []SpecPart {
	return []SpecPart{PartCharacter, PartProduct, PartLocation, PartActing, PartCamera, PartLighting}
}

// Valid reports whether p is a known part.
func (p SpecPart) Valid() bool { return slices.Contains(SpecParts(), p) }

// Part returns the part of the Shot Spec that the parameter fixes.
func (p LockParameter) Part() (SpecPart, bool) {
	switch p {
	case ParamCharacterIdentity:
		return PartCharacter, true
	case ParamProductGeometry:
		return PartProduct, true
	case ParamActing:
		return PartActing, true
	case ParamCamera:
		return PartCamera, true
	case ParamLighting:
		return PartLighting, true
	}
	return "", false
}

// LockSetting is the mode of one parameter and its value: the model name of Video Model, when the
// user chose one (MANUAL) or the current one was frozen (LOCK).
type LockSetting struct {
	Mode  LockMode
	Value json.RawMessage
}

// ParameterLocks is the AUTO / MANUAL / LOCK setting of the parameters of one Shot or Project.
// A parameter without an entry is AUTO.
type ParameterLocks map[LockParameter]LockSetting

// Mode returns the mode of the parameter.
func (l ParameterLocks) Mode(p LockParameter) LockMode {
	if s, ok := l[p]; ok && s.Mode.Valid() {
		return s.Mode
	}
	return LockAuto
}

// Locked reports whether the parameter is LOCKed.
func (l ParameterLocks) Locked(p LockParameter) bool { return l.Mode(p) == LockLocked }

// Text returns the value of the parameter as a string ("" when it has none).
func (l ParameterLocks) Text(p LockParameter) string {
	var s string
	_ = json.Unmarshal(l[p].Value, &s)
	return s
}

// PartsWith returns the parts of the Shot Spec whose parameter is in the given mode.
func (l ParameterLocks) PartsWith(mode LockMode) []SpecPart {
	var parts []SpecPart
	for _, p := range ShotLockParameters() {
		if part, ok := p.Part(); ok && l.Mode(p) == mode {
			parts = append(parts, part)
		}
	}
	return parts
}

// LockedParts returns the parts of the Shot Spec that are LOCKed.
func (l ParameterLocks) LockedParts() []SpecPart { return l.PartsWith(LockLocked) }

func specPart(s ShotSpec, p SpecPart) any {
	switch p {
	case PartCharacter:
		return s.Character
	case PartProduct:
		return s.Product
	case PartLocation:
		return s.Location
	case PartActing:
		return s.Acting
	case PartCamera:
		return s.Camera
	case PartLighting:
		return s.Lighting
	}
	return nil
}

// LockedChanges returns the LOCKed parameters that a change from one Shot Spec to another touches.
// Prompt covers the whole spec, so any change touches it; the other parameters cover their part.
func LockedChanges(l ParameterLocks, from, to ShotSpec) []LockParameter {
	var hit []LockParameter
	for _, p := range ShotLockParameters() {
		if !l.Locked(p) {
			continue
		}
		part, ok := p.Part()
		switch {
		case ok && !sameJSON(specPart(from, part), specPart(to, part)):
			hit = append(hit, p)
		case p == ParamPrompt && !sameJSON(from, to):
			hit = append(hit, p)
		}
	}
	return hit
}

// sameJSON compares two values by their JSON form, so an absent list and an empty one are equal.
func sameJSON(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(emptyJSON(x), emptyJSON(y))
}

// emptyJSON maps the JSON of an empty list to that of a missing one.
func emptyJSON(b []byte) []byte {
	if string(b) == "[]" {
		return []byte("null")
	}
	return b
}

// PreserveParts returns planned with the given parts taken from current: what a regeneration must
// not change. Character and Product also carry the constraint that keeps the reference look.
func PreserveParts(planned, current ShotSpec, parts []SpecPart) ShotSpec {
	planned.Constraints = slices.Clone(planned.Constraints)
	for _, p := range parts {
		switch p {
		case PartCharacter:
			planned.Character = current.Character
			planned.Constraints = withConstraint(planned.Constraints, "preserve_face_identity")
		case PartProduct:
			planned.Product = current.Product
			planned.Constraints = withConstraint(planned.Constraints, "preserve_product_geometry")
		case PartLocation:
			planned.Location = current.Location
		case PartActing:
			planned.Acting = current.Acting
		case PartCamera:
			planned.Camera = current.Camera
		case PartLighting:
			planned.Lighting = current.Lighting
		}
	}
	return planned
}

func withConstraint(list []string, c string) []string {
	if slices.Contains(list, c) {
		return list
	}
	return append(list, c)
}

// Parameter returns the parameter that fixes the part; Location (the environment) has none.
func (p SpecPart) Parameter() (LockParameter, bool) {
	for _, lp := range ShotLockParameters() {
		if part, ok := lp.Part(); ok && part == p {
			return lp, true
		}
	}
	return "", false
}

// Label is the part as the user sees it.
func (p SpecPart) Label() string {
	switch p {
	case PartCharacter:
		return "Character"
	case PartProduct:
		return "Product"
	case PartLocation:
		return "Environment"
	case PartActing:
		return "Acting"
	case PartCamera:
		return "Camera"
	case PartLighting:
		return "Lighting"
	}
	return string(p)
}
