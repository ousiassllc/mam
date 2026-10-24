package worldstate

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func registerLocks(r *mcpkit.Registrar, svc *service.WorldService) {
	mcpkit.Write(r, "set_world_field_lock",
		"World State の項目を AUTO と LOCK で切り替える。LOCK した項目は State Extractor が更新せず、編集もできない / Switch one World State attribute between AUTO and LOCK.",
		setFieldLock(svc), mcpkit.Idempotent())
	registerAssign(r, svc,
		"assign_character_reference", "登場人物の参照枠（face か body）に Reference Library の画像を割り当てる。reference_id を空にすると枠を空にする。枠が LOCK 中なら locked / Put a Library image on the face or body slot of a Character (empty reference_id clears it).",
		domain.TargetCharacter)
	registerAssign(r, svc,
		"assign_product_reference", "商品の参照枠（geometry か label）に Reference Library の画像を割り当てる。reference_id を空にすると枠を空にする / Put a Library image on the geometry or label slot of a Product.",
		domain.TargetProduct)
	registerAssign(r, svc,
		"assign_location_reference", "場所の参照枠（layout）に Reference Library の画像を割り当てる。reference_id を空にすると枠を空にする / Put a Library image on the layout slot of a Location.",
		domain.TargetLocation)
}

// SetFieldLockInput names the attribute and the mode.
type SetFieldLockInput struct {
	Scope
	TargetType string `json:"target_type" jsonschema:"character, product, location, camera, story"`
	TargetID   string `json:"target_id,omitempty" jsonschema:"character / product / location の ID。camera と story では不要"`
	Parameter  string `json:"parameter" jsonschema:"LOCK する属性。character: hairstyle, wardrobe, voice, personality, emotion, position, current_action, identity。product: material, orientation, condition, geometry, label。location: time, lighting_state, weather, layout_reference。camera: lens, position, angle, movement。story: scene, character_goal, known_information, previous_event"`
	Mode       string `json:"mode" jsonschema:"auto か lock"`
}

func setFieldLock(svc *service.WorldService) mcpkit.Handler[SetFieldLockInput, View] {
	return func(ctx context.Context, in SetFieldLockInput) (View, error) {
		v, err := svc.SetFieldLock(ctx, in.ProjectID, in.BranchID, domain.WorldTarget(in.TargetType), in.TargetID, in.Parameter, domain.LockMode(in.Mode))
		if err != nil {
			return View{}, err
		}
		return ViewOf(v), nil
	}
}

// AssignReferenceInput names the entity, the slot and the Library image.
type AssignReferenceInput struct {
	Scope
	EntityID    string `json:"entity_id" jsonschema:"登場人物・商品・場所の ID（get_world_state の id）"`
	Slot        string `json:"slot" jsonschema:"参照枠。character: face か body。product: geometry か label。location: layout"`
	ReferenceID string `json:"reference_id,omitempty" jsonschema:"Reference Library の ID（種別が枠に合う画像）。空で枠を空にする"`
}

func registerAssign(r *mcpkit.Registrar, svc *service.WorldService, name, description string, target domain.WorldTarget) {
	mcpkit.Write(r, name, description, func(ctx context.Context, in AssignReferenceInput) (View, error) {
		v, err := svc.AssignReference(ctx, in.ProjectID, in.BranchID, target, in.EntityID, in.Slot, in.ReferenceID)
		if err != nil {
			return View{}, err
		}
		return ViewOf(v), nil
	})
}
