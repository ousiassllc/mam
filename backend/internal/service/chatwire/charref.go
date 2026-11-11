package chatwire

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/charref"
)

// The chat operation that puts an attached image on the face or body of a Character (service/chatops/charref, #1140): the World State is WorldService.View, the assignment
// is WorldService.AssignReference (the screen and the MCP tool assign_character_reference), and the image enters the Reference Library through addFromAsset (ReferenceService.Upload with the stored file),
// which is the Library's upload with the file the chat already stored.
func init() {
	builders = append(builders, func(env Env, reg *chatops.Runtime) {
		if env.World == nil || env.Reference == nil {
			return
		}
		world, library := env.World, env.Reference
		reg.Handle(charref.OpAssign, charref.New(charref.Deps{
			Characters: func(ctx context.Context, projectID string) ([]charref.Character, error) {
				v, err := world.View(ctx, projectID, "")
				if err != nil {
					return nil, err
				}
				out := make([]charref.Character, 0, len(v.World.Characters))
				for _, c := range v.World.Characters {
					ch := charref.Character{ID: c.ID, Name: c.Name, Has: map[string]bool{}, Locked: map[string]bool{}}
					for _, slot := range []string{charref.SlotFace, charref.SlotBody} {
						s, _ := domain.FindReferenceSlot(domain.TargetCharacter, slot)
						ch.Locked[slot] = v.Locks.Locked(domain.TargetCharacter, c.ID, s.Lock)
					}
					ch.Has[charref.SlotFace], ch.Has[charref.SlotBody] = c.FaceReferenceAssetID != "", c.BodyReferenceAssetID != ""
					out = append(out, ch)
				}
				return out, nil
			},
			AddReference: func(ctx context.Context, projectID, assetID, name string) (string, error) {
				item, err := addFromAsset(ctx, library, projectID, assetID, domain.RefCharacter, name)
				if err != nil {
					return "", charrefRefusal(err)
				}
				return item.ID, nil
			},
			RemoveReference: func(ctx context.Context, projectID, referenceID string) error {
				_, err := library.Remove(ctx, projectID, referenceID, service.LibraryQuery{})
				return err
			},
			Assign: func(ctx context.Context, projectID, characterID, slot, referenceID string) error {
				_, err := world.AssignReference(ctx, projectID, "", domain.TargetCharacter, characterID, slot, referenceID)
				return charrefRefusal(err)
			},
		}))
	})
}

// charrefRefusal turns what the screen refuses (a LOCKed slot, a Character or an image that is gone, an image the Library does not take) into *chatops.Refusal.
func charrefRefusal(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, service.ErrLocked):
		return &chatops.Refusal{Message: "LOCK されている項目は変更できません。LOCK を解除してから指示してください"}
	case errors.Is(err, service.ErrNotFound):
		return &chatops.Refusal{Message: "Character か画像が見つかりません。画像を添付して、もう一度送ってください"}
	}
	return refusalOf(err)
}
