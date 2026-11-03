package voicerefsvc

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/characterrepo"
)

// MaxCharacterNameRunes bounds the name of a Character made by hand (the name is what the State Extractor and the Reference Pack find a
// Character by).
const MaxCharacterNameRunes = 100

// NewCharacter is what a Character made by hand (the screen's "登場人物を追加", the MCP tool create_character) is made of: the name, the
// look and voice the canon keeps, and the Reference Library entries of the face and the body ("" = none). It has no emotion, position or
// action: those are what a Shot leaves.
type NewCharacter struct {
	Name, Hairstyle, Wardrobe, Voice, Personality string
	// FaceReferenceID and BodyReferenceID are entries of the Reference Library of a Project (kind character).
	FaceReferenceID, BodyReferenceID string
}

// row trims and checks the input (a name of 1 to MaxCharacterNameRunes characters, texts of at most domain.MaxWorldValueRunes, the
// limit of an edit) and returns the row with its new ID and the asset IDs of the face and the body. Anything else is an InvalidError.
func (n NewCharacter) row(face, body string) (domain.WorldCharacter, error) {
	c := domain.WorldCharacter{
		ID: domain.NewID(), Name: strings.TrimSpace(n.Name), Hairstyle: strings.TrimSpace(n.Hairstyle), Wardrobe: strings.TrimSpace(n.Wardrobe),
		Voice: strings.TrimSpace(n.Voice), Personality: strings.TrimSpace(n.Personality), FaceReferenceAssetID: face, BodyReferenceAssetID: body,
	}
	if c.Name == "" || utf8.RuneCountInString(c.Name) > MaxCharacterNameRunes {
		return c, invalid("name", "名前は 1〜%d 文字で入力してください", MaxCharacterNameRunes)
	}
	for name, value := range map[string]string{"hairstyle": c.Hairstyle, "wardrobe": c.Wardrobe, "voice": c.Voice, "personality": c.Personality} {
		if utf8.RuneCountInString(value) > domain.MaxWorldValueRunes {
			return c, invalid(name, "%d 文字以内で入力してください", domain.MaxWorldValueRunes)
		}
	}
	return c, nil
}

// image is the asset of a Library entry of the Project that fits the slot of a Character ("" for no entry): ErrNotFound when the entry
// is not in the Library of the Project, an InvalidError when it is an image of another kind (a Style image on a face), as assigning one does.
func (s *Service) image(ctx context.Context, projectID, slotName, field, entryID string) (string, error) {
	if entryID = strings.TrimSpace(entryID); entryID == "" {
		return "", nil
	}
	slot, _ := domain.FindReferenceSlot(domain.TargetCharacter, slotName)
	item, err := s.refs.Get(ctx, projectID, entryID)
	if err != nil {
		return "", notFound(err)
	}
	if item.Kind != slot.Kind {
		return "", invalid(field, "%s の参照画像を選んでください", slot.Kind.Label())
	}
	return item.AssetID, nil
}

// images is the face and the body of the input, as the assets of the Library of the Project.
func (s *Service) images(ctx context.Context, projectID string, n NewCharacter) (face, body string, err error) {
	if face, err = s.image(ctx, projectID, "face", "face_reference_id", n.FaceReferenceID); err != nil {
		return "", "", err
	}
	body, err = s.image(ctx, projectID, "body", "body_reference_id", n.BodyReferenceID)
	return face, body, err
}

// taken is the InvalidError of a name that a Character of the same World State or canon already has.
func taken(err error) error {
	if errors.Is(err, repository.ErrConflict) {
		return invalid("name", "同じ名前の登場人物がすでにあります")
	}
	return notFound(err)
}

// CreateForProject makes a Character by hand in the World State of the Project (of the Branch when branchID is set), as a new version of
// it, and returns the ID of the Character, so that a face or a body can be assigned to it later without the Project's planning having
// made one. in.FaceReferenceID and in.BodyReferenceID, when set, are entries of the Reference Library of the Project. An unknown Project,
// Branch or Library entry is ErrNotFound; a bad name or text, a Library image of the wrong kind, or a name another Character of the World
// State has is an InvalidError.
func (s *Service) CreateForProject(ctx context.Context, projectID, branchID string, in NewCharacter) (string, error) {
	if err := s.exists(ctx, owner{id: projectID}); err != nil {
		return "", err
	}
	face, body, err := s.images(ctx, projectID, in)
	if err != nil {
		return "", err
	}
	c, err := in.row(face, body)
	if err != nil {
		return "", err
	}
	if err := characterrepo.InWorld(ctx, s.world, projectID, branchID, c); err != nil {
		return "", taken(err)
	}
	return c.ID, nil
}

// CreateForSeries makes a Character by hand in the canon of the Series, as a new version of it (Episodes that already copied the canon are
// not touched; the next Episode gets the Character), and returns its ID. The Series has no Reference Library: in.FaceReferenceID and
// in.BodyReferenceID, when set, are entries of the Library of the Project referenceProjectID (where the images were registered), whose
// images the canon then keeps as the canon made from a Project does; without referenceProjectID they are an InvalidError. An unknown
// Series, Project or entry is ErrNotFound; a bad name or text, an image of the wrong kind, or a name the canon already has is an InvalidError.
func (s *Service) CreateForSeries(ctx context.Context, seriesID, referenceProjectID string, in NewCharacter) (string, error) {
	if err := s.exists(ctx, owner{id: seriesID, series: true}); err != nil {
		return "", err
	}
	var face, body string
	if strings.TrimSpace(in.FaceReferenceID+in.BodyReferenceID) != "" {
		if referenceProjectID = strings.TrimSpace(referenceProjectID); referenceProjectID == "" {
			return "", invalid("reference_project_id", "参照画像を選ぶときは、その画像を登録した Project を指定してください")
		}
		err := s.exists(ctx, owner{id: referenceProjectID})
		if err == nil {
			face, body, err = s.images(ctx, referenceProjectID, in)
		}
		if err != nil {
			return "", err
		}
	}
	c, err := in.row(face, body)
	if err != nil {
		return "", err
	}
	if err := characterrepo.InCanon(ctx, s.series, seriesID, c); err != nil {
		return "", taken(err)
	}
	return c.ID, nil
}
