// Package worldstate renders the World State view (GET /projects/:id/world-state) and the answers of
// the requests made on it. Each request answers with the part it changed, not with the whole view.
package worldstate

import (
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Data is the state of the World State view (world_state_page).
type Data struct {
	Project domain.Project
	// Branch is the Branch the view shows; nil for the trunk.
	Branch *domain.Branch
	World  domain.WorldContext
	Locks  domain.LockSet
	// ProjectLocks is the AUTO / MANUAL / LOCK setting of Story and Music.
	ProjectLocks domain.ParameterLocks
	// Library is the Reference Library of the Project (images a reference slot can hold).
	Library []domain.ReferenceItem
	// Versions are the saved World State versions of the trunk, newest first (F-28).
	Versions []domain.VersionEntry
}

var fieldLabels = map[string]string{
	"hairstyle": "髪型 (hairstyle)", "wardrobe": "衣装 (wardrobe)", "voice": "声 (voice)", "personality": "性格 (personality)",
	"emotion": "感情 (emotion)", "position": "位置 (position)", "current_action": "動作 (current_action)",
	"material": "素材 (material)", "orientation": "向き (orientation)", "condition": "状態 (condition)",
	"time": "時間帯 (time)", "lighting_state": "照明 (lighting_state)", "weather": "天気 (weather)",
	"lens": "レンズ (lens)", "angle": "アングル (angle)", "movement": "動き (movement)",
	"character_goal": "目的 (character_goal)", "known_information": "既知の情報 (known_information)", "previous_event": "直前の出来事 (previous_event)",
}

func fieldLabel(name string) string {
	if l, ok := fieldLabels[name]; ok {
		return l
	}
	return name
}

// branchID is the ID of the Branch of the view ("" for the trunk).
func (d Data) branchID() string {
	if d.Branch == nil {
		return ""
	}
	return d.Branch.ID
}

func (d Data) entityURL(plural, id string) string {
	return paths.Project(d.Project.ID, plural, id)
}

func (d Data) field(target domain.WorldTarget, id, name, value string) molecules.WorldStateFieldData {
	return molecules.WorldStateFieldData{
		ID: "ws-" + string(target) + "-" + id + "-" + name, Name: name, Label: fieldLabel(name), Value: value,
		Locked: d.Locks.Locked(target, id, name),
	}
}

// entityView is what the forms of one Character, Product or Location need.
type entityView struct {
	Target domain.WorldTarget
	// Plural is the collection of the entity in the URLs ("characters", "products", "locations").
	Plural string
	ID     string
	Name   string
	Fields []molecules.WorldStateFieldData
	// Slots is the asset ID now in each reference slot, by slot name.
	Slots map[string]string
	// VoiceRef is the reference voice of a Character (nil for none and for the other entities).
	VoiceRef *voiceref.Reference
}

// formID is the id of the attribute form of the entity; a save replaces that form only.
func (e entityView) formID() string { return "ws-form-" + string(e.Target) + "-" + e.ID }

func (e entityView) saveID() string { return "ws-save-" + string(e.Target) + "-" + e.ID }

func (e entityView) slotFormID(slot string) string {
	return "ws-slot-form-" + string(e.Target) + "-" + e.ID + "-" + slot
}

func characterView(d Data, c domain.WorldCharacter) entityView {
	values := map[string]string{
		"hairstyle": c.Hairstyle, "wardrobe": c.Wardrobe, "voice": c.Voice, "personality": c.Personality,
		"emotion": c.Emotion, "position": c.Position, "current_action": c.CurrentAction,
	}
	return entityView{
		Target: domain.TargetCharacter, Plural: "characters", ID: c.ID, Name: c.Name,
		Fields: entityFields(d, domain.TargetCharacter, c.ID, values),
		Slots:  map[string]string{"face": c.FaceReferenceAssetID, "body": c.BodyReferenceAssetID}, VoiceRef: c.VoiceReference,
	}
}

func productView(d Data, p domain.WorldProduct) entityView {
	return entityView{
		Target: domain.TargetProduct, Plural: "products", ID: p.ID, Name: p.Name,
		Fields: entityFields(d, domain.TargetProduct, p.ID, map[string]string{"material": p.Material, "orientation": p.Orientation, "condition": p.Condition}),
		Slots:  map[string]string{"geometry": p.GeometryReferenceAssetID, "label": p.LabelReferenceAssetID},
	}
}

func locationView(d Data, l domain.WorldLocation) entityView {
	return entityView{
		Target: domain.TargetLocation, Plural: "locations", ID: l.ID, Name: l.Name,
		Fields: entityFields(d, domain.TargetLocation, l.ID, map[string]string{"time": l.Time, "lighting_state": l.LightingState, "weather": l.Weather}),
		Slots:  map[string]string{"layout": l.LayoutReferenceAssetID},
	}
}

// entity finds the Character, Product or Location of the view; ok is false when there is none.
func (d Data) entity(target domain.WorldTarget, id string) (entityView, bool) {
	switch target {
	case domain.TargetCharacter:
		for _, c := range d.World.Characters {
			if c.ID == id {
				return characterView(d, c), true
			}
		}
	case domain.TargetProduct:
		for _, p := range d.World.Products {
			if p.ID == id {
				return productView(d, p), true
			}
		}
	case domain.TargetLocation:
		for _, l := range d.World.Locations {
			if l.ID == id {
				return locationView(d, l), true
			}
		}
	}
	return entityView{}, false
}

func entityFields(d Data, target domain.WorldTarget, id string, values map[string]string) []molecules.WorldStateFieldData {
	names := domain.EditableFields(target)
	out := make([]molecules.WorldStateFieldData, len(names))
	for i, name := range names {
		out[i] = d.field(target, id, name, values[name])
	}
	return out
}

// fieldLockToggle is the AUTO / LOCK switch of one attribute (PUT /projects/:id/world-state/locks).
// The switch replaces itself only (LockSwitched carries what else the LOCK changes).
func (d Data) fieldLockToggle(target domain.WorldTarget, id, field string) molecules.LockToggleData {
	mode := domain.LockAuto
	if d.Locks.Locked(target, id, field) {
		mode = domain.LockLocked
	}
	hidden := []molecules.HiddenField{{Name: "target_type", Value: string(target)}}
	if id != "" {
		hidden = append(hidden, molecules.HiddenField{Name: "target_id", Value: id})
	}
	return molecules.LockToggleData{
		DOMID: "lock-" + string(target) + "-" + id + "-" + field, URL: paths.WithBranch(paths.Project(d.Project.ID, "world-state", "locks"), d.branchID()),
		Parameter: domain.LockParameter(field), Label: lockLabel(field), Mode: mode,
		Modes: []domain.LockMode{domain.LockAuto, domain.LockLocked}, Hidden: hidden,
	}
}

var lockOnlyLabels = map[string]string{
	domain.LockParamIdentity: "顔・見た目 (identity)", domain.LockParamGeometry: "形状 (geometry)",
	domain.LockParamLabel: "ロゴ・ラベル (label)", domain.LockFieldLayoutReference: "レイアウト参照 (layout_reference)",
	domain.StoryFieldScene: "Scene (scene)",
}

func lockLabel(field string) string {
	if l, ok := lockOnlyLabels[field]; ok {
		return l
	}
	return fieldLabel(field)
}

// slotLabels names the reference slots.
var slotLabels = map[string]string{"face": "顔 (face)", "body": "全身 (body)", "geometry": "形状 (geometry)", "label": "ラベル (label)", "layout": "レイアウト (layout)"}

// slotForm is one reference slot of an entity: the current image and the Library images of the fitting kind.
type slotForm struct {
	Slot    domain.ReferenceSlot
	Current string // the asset ID now in the slot
	Items   []domain.ReferenceItem
}

func (d Data) slots(target domain.WorldTarget, current map[string]string) []slotForm {
	var out []slotForm
	for _, s := range domain.ReferenceSlots() {
		if s.Target != target {
			continue
		}
		f := slotForm{Slot: s, Current: current[s.Name]}
		for _, it := range d.Library {
			if it.Kind == s.Kind {
				f.Items = append(f.Items, it)
			}
		}
		out = append(out, f)
	}
	return out
}

func (d Data) empty() bool {
	return len(d.World.Characters)+len(d.World.Products)+len(d.World.Locations) == 0 && d.World.Version == 0
}

func versionText(v int) string {
	if v == 0 {
		return "まだ Shot が完了していません"
	}
	return "World State バージョン " + strconv.Itoa(v)
}

func storyScene(s domain.WorldStory) string {
	if s.Scene == 0 {
		return ""
	}
	return strconv.Itoa(s.Scene)
}

func joinKnown(items []string) string { return strings.Join(items, " / ") }
