package charref

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// Character is a Character of the World State as the operation needs it.
type Character struct {
	ID, Name string
	// Has says whether the slot ("face", "body") holds an image now.
	Has map[string]bool
	// Locked says whether the slot is LOCKed (the identity LOCK of the World State screen): an assignment is then refused.
	Locked map[string]bool
}

// Deps is what the operation needs of the service layer: the World State and Reference Library services the World State screen and the MCP tool assign_character_reference
// use. The functions report what the screen reports (a LOCKed slot, an image that is no image) as *chatops.Refusal.
type Deps struct {
	// Characters are the Characters of the Project's World State (the trunk).
	Characters func(ctx context.Context, projectID string) ([]Character, error)
	// AddReference registers the stored image (an asset of the Project) in the Reference Library as a Character reference called name and returns its ID.
	AddReference func(ctx context.Context, projectID, assetID, name string) (referenceID string, err error)
	// RemoveReference takes the entry out of the Library again (an assignment that failed leaves nothing behind).
	RemoveReference func(ctx context.Context, projectID, referenceID string) error
	// Assign puts the Library entry on the slot of the Character (WorldService.AssignReference).
	Assign func(ctx context.Context, projectID, characterID, slot, referenceID string) error
}

// Handler is the Handler of OpAssign.
type Handler struct{ deps Deps }

// New returns the handler of OpAssign.
func New(d Deps) *Handler { return &Handler{deps: d} }

const (
	needImage = "参照にする画像が添付されていません。画像を添付して、同じメッセージで「この画像を主人公の顔にして」のように送ってください（画像は 1 枚ずつ、jpg / png / webp / gif）。"
	needSlot  = "顔（face）と体（body）のどちらの参照にするか教えてください。例:「この画像を主人公の顔にして」「この画像をミオの全身の参照にして」"
)

func slotLabel(slot string) string {
	if slot == SlotBody {
		return "体（body）"
	}
	return "顔（face）"
}

// slotShort is the slot in the name of the Library entry: 「ミオ（顔）」.
func slotShort(slot string) string {
	if slot == SlotBody {
		return "体"
	}
	return "顔"
}

// Question builds the card. It reads the image from this turn's attachments (the button's answer comes later and carries none, so the card's yes button carries the asset) and
// resolves the Character. It changes nothing.
func (h *Handler) Question(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Card{}, err
	}
	slot := slotOf(a.Slot)
	if slot == "" {
		return chatops.Card{}, chatops.Refuse("%s", needSlot)
	}
	asset, err := attachedImage(ctx)
	if err != nil {
		return chatops.Card{}, err
	}
	chars, err := h.deps.Characters(ctx, project.ID)
	if err != nil {
		return chatops.Card{}, err
	}
	if len(chars) == 0 {
		return chatops.Card{}, chatops.Refuse("この Project の World State にはまだ Character がいません。登場人物が決まってから（最初の Shot のあと）、もう一度お願いします")
	}
	found := resolve(chars, a.Character)
	if len(found) == 0 {
		return chatops.Card{}, chatops.Refuse("「%s」という Character は見つかりません。この Project の Character: %s", a.Character, names(chars))
	}
	open := make([]Character, 0, len(found))
	for _, c := range found {
		if !c.Locked[slot] {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return chatops.Card{}, refuseLocked(found[0], slot)
	}
	if len(open) > chatops.MaxChoices {
		return chatops.Card{}, chatops.Refuse("Character が多すぎて選べません（%d 人）。Character の名前を言ってもう一度送ってください。候補: %s", len(open), names(open[:chatops.MaxChoices]))
	}
	if len(open) > 1 {
		return choices(open, slot, asset.ID)
	}
	chosen, err := commandinterpreter.NewOperation(OpAssign, Args{Character: open[0].Name, CharacterID: open[0].ID, Slot: slot, AssetID: asset.ID})
	if err != nil {
		return chatops.Card{}, err
	}
	return chatops.Card{Text: cardText(open[0], slot), Op: &chosen}, nil
}

func cardText(c Character, slot string) string {
	text := "この画像を「" + c.Name + "」の" + slotLabel(slot) + "の参照にします。\n" +
		"・画像を Reference Library に Character の参照画像「" + libraryName(c.Name, slot) + "」として登録し、" + c.Name + " の " + slot + " 枠に割り当てます。\n"
	if c.Has[slot] {
		text += "・いま " + slot + " 枠にある画像は外れます。\n"
	}
	return text + "・この Project の Character だけが変わります（ほかのプロジェクトには影響しません）。\n" +
		"・以降の Shot の生成で、この画像が " + c.Name + " の" + slotShort(slot) + "の参照として使われます。できあがった Take は変わりません。"
}

// choices is the card that asks which Character: each button is that Character's assignment, so picking one carries it out.
func choices(found []Character, slot, assetID string) (chatops.Card, error) {
	card := chatops.Card{Text: "この画像をどの Character の" + slotLabel(slot) + "の参照にしますか？\n" +
		"・選んだ Character の " + slot + " 枠に割り当て、Reference Library にも Character の参照画像として登録します。\n" +
		"・この Project の Character だけが変わります（ほかのプロジェクトには影響しません）。"}
	for _, c := range found {
		op, err := commandinterpreter.NewOperation(OpAssign, Args{Character: c.Name, CharacterID: c.ID, Slot: slot, AssetID: assetID})
		if err != nil {
			return chatops.Card{}, err
		}
		card.Choices = append(card.Choices, chatops.Choice{Label: c.Name + " の" + slotShort(slot) + "にする", Op: op})
	}
	return card, nil
}

// attachedImage is the one image of this message; a message without one, or with several, is refused (the card must say which image it uses).
func attachedImage(ctx context.Context) (domain.Asset, error) {
	var images []domain.Asset
	for _, a := range chatops.Attachments(ctx) {
		if a.Type == domain.AssetImage {
			images = append(images, a)
		}
	}
	switch len(images) {
	case 0:
		return domain.Asset{}, chatops.Refuse("%s", needImage)
	case 1:
		return images[0], nil
	}
	return domain.Asset{}, chatops.Refuse("画像が %d 枚添付されています。参照にする画像を 1 枚だけ添付して送ってください", len(images))
}

func refuseLocked(c Character, slot string) error {
	return chatops.Refuse("%s の%sの参照枠は LOCK されているため変更できません。LOCK を解除してから指示してください", c.Name, slotShort(slot))
}

// Apply registers the image in the Reference Library and assigns it, through the same services as the screen and the MCP tool. The slot is checked again (it may have been
// LOCKed since the card), and a failed assignment takes the new Library entry out again.
func (h *Handler) Apply(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Result{}, err
	}
	slot := slotOf(a.Slot)
	if slot == "" || a.CharacterID == "" || a.AssetID == "" {
		return chatops.Result{}, chatops.Refuse("この操作の内容が足りません。画像を添付して、もう一度送ってください")
	}
	chars, err := h.deps.Characters(ctx, project.ID)
	if err != nil {
		return chatops.Result{}, err
	}
	var c Character
	for _, candidate := range chars {
		if candidate.ID == a.CharacterID {
			c = candidate
		}
	}
	if c.ID == "" {
		return chatops.Result{}, chatops.Refuse("その Character はもういません。画像を添付して、もう一度送ってください")
	}
	if c.Locked[slot] {
		return chatops.Result{}, refuseLocked(c, slot)
	}
	refID, err := h.deps.AddReference(ctx, project.ID, a.AssetID, libraryName(c.Name, slot))
	if err != nil {
		return chatops.Result{}, err
	}
	if err := h.deps.Assign(ctx, project.ID, c.ID, slot, refID); err != nil {
		if rmErr := h.deps.RemoveReference(context.WithoutCancel(ctx), project.ID, refID); rmErr != nil {
			// the failure of the assignment is the one the user must hear (a refusal stays one); the Library entry left behind is joined to it
			return chatops.Result{}, errors.Join(err, fmt.Errorf("remove the Library entry %s: %w", refID, rmErr))
		}
		return chatops.Result{}, err
	}
	return chatops.Result{Text: "「" + c.Name + "」の" + slotLabel(slot) + "の参照を設定しました。画像は Reference Library に「" + libraryName(c.Name, slot) + "」として登録しています。" +
		"以降の Shot の生成から使われます。この Project の Character だけが変わりました。"}, nil
}

// libraryName is the name of the Library entry, cut to what the Library takes.
func libraryName(name, slot string) string {
	suffix := "（" + slotShort(slot) + "）"
	for utf8.RuneCountInString(name+suffix) > domain.MaxReferenceNameRunes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name + suffix
}

func names(chars []Character) string {
	out := make([]string, len(chars))
	for i, c := range chars {
		out[i] = "「" + c.Name + "」"
	}
	return strings.Join(out, "、")
}

// roleWords name "the" Character of a Project that has only one.
var roleWords = []string{"主人公", "主役", "メイン", "ヒロイン", "主人", "本人", "彼女", "彼", "この人", "キャラ"}

// resolve finds the Characters the director named: the name as it is, else a name that holds it or is part of it. A name that is none ("" or a role word such as 主人公) is every Character, so that the card takes the only one or asks which.
func resolve(chars []Character, name string) []Character {
	key := fold(name)
	if key == "" {
		return chars
	}
	var exact, partial []Character
	for _, c := range chars {
		switch n := fold(c.Name); {
		case n == key:
			exact = append(exact, c)
		case utf8.RuneCountInString(key) >= 2 && utf8.RuneCountInString(n) >= 2 && (strings.Contains(n, key) || strings.Contains(key, n)):
			partial = append(partial, c)
		}
	}
	switch {
	case len(exact) > 0:
		return exact
	case len(partial) > 0:
		return partial
	case isRole(key):
		return chars
	}
	return nil
}

func isRole(key string) bool {
	for _, w := range roleWords {
		if key == w {
			return true
		}
	}
	return false
}

// fold is a name without blanks and case, for comparing.
func fold(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(s, "　", " ")), ""))
}
