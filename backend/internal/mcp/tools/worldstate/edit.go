package worldstate

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func registerEdits(r *mcpkit.Registrar, svc *service.WorldService) {
	mcpkit.Write(r, "update_character",
		"登場人物の属性（髪型・衣装・声・性格・感情・位置・動作）を、渡した項目だけ更新する。LOCK 中の項目は locked で拒否 / Update the given attributes of a Character. A LOCKed attribute is refused (locked). 空文字で項目を空にする。",
		updateCharacter(svc))
	mcpkit.Write(r, "update_product",
		"商品の属性（素材・向き・状態）を、渡した項目だけ更新する。LOCK 中の項目は locked で拒否 / Update the given attributes of a Product.",
		updateProduct(svc))
	mcpkit.Write(r, "update_location",
		"場所の属性（時間帯・照明・天候）を、渡した項目だけ更新する。LOCK 中の項目は locked で拒否 / Update the given attributes of a Location.",
		updateLocation(svc))
}

// UpdateCharacterInput names the Character and the attributes to set; an attribute left out stays as it is.
type UpdateCharacterInput struct {
	Scope
	CharacterID   string  `json:"character_id"`
	Hairstyle     *string `json:"hairstyle,omitempty"`
	Wardrobe      *string `json:"wardrobe,omitempty"`
	Voice         *string `json:"voice,omitempty"`
	Personality   *string `json:"personality,omitempty"`
	Emotion       *string `json:"emotion,omitempty"`
	Position      *string `json:"position,omitempty"`
	CurrentAction *string `json:"current_action,omitempty"`
}

// UpdateProductInput names the Product and the attributes to set; an attribute left out stays as it is.
type UpdateProductInput struct {
	Scope
	ProductID   string  `json:"product_id"`
	Material    *string `json:"material,omitempty"`
	Orientation *string `json:"orientation,omitempty"`
	Condition   *string `json:"condition,omitempty"`
}

// UpdateLocationInput names the Location and the attributes to set; an attribute left out stays as it is.
type UpdateLocationInput struct {
	Scope
	LocationID    string  `json:"location_id"`
	Time          *string `json:"time,omitempty"`
	LightingState *string `json:"lighting_state,omitempty"`
	Weather       *string `json:"weather,omitempty"`
}

func updateCharacter(svc *service.WorldService) mcpkit.Handler[UpdateCharacterInput, View] {
	return func(ctx context.Context, in UpdateCharacterInput) (View, error) {
		fields := given(map[string]*string{
			"hairstyle": in.Hairstyle, "wardrobe": in.Wardrobe, "voice": in.Voice, "personality": in.Personality,
			"emotion": in.Emotion, "position": in.Position, "current_action": in.CurrentAction,
		})
		return updateEntity(ctx, svc, in.Scope, domain.TargetCharacter, in.CharacterID, fields)
	}
}

func updateProduct(svc *service.WorldService) mcpkit.Handler[UpdateProductInput, View] {
	return func(ctx context.Context, in UpdateProductInput) (View, error) {
		fields := given(map[string]*string{"material": in.Material, "orientation": in.Orientation, "condition": in.Condition})
		return updateEntity(ctx, svc, in.Scope, domain.TargetProduct, in.ProductID, fields)
	}
}

func updateLocation(svc *service.WorldService) mcpkit.Handler[UpdateLocationInput, View] {
	return func(ctx context.Context, in UpdateLocationInput) (View, error) {
		fields := given(map[string]*string{"time": in.Time, "lighting_state": in.LightingState, "weather": in.Weather})
		return updateEntity(ctx, svc, in.Scope, domain.TargetLocation, in.LocationID, fields)
	}
}

// given keeps the attributes the caller sent. The service validates them (editable, length) and the LOCKs.
func given(all map[string]*string) map[string]string {
	fields := make(map[string]string, len(all))
	for name, v := range all {
		if v != nil {
			fields[name] = *v
		}
	}
	return fields
}

func updateEntity(ctx context.Context, svc *service.WorldService, s Scope, target domain.WorldTarget, id string, fields map[string]string) (View, error) {
	v, err := svc.UpdateEntity(ctx, s.ProjectID, s.BranchID, target, id, fields)
	if err != nil {
		return View{}, err
	}
	return ViewOf(v), nil
}
