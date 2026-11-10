package chattest

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
)

var charrefPNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

// newCharrefFixture is a Project that is being directed, with a Character "Mio" in its World State and the chat operations wired as the app wires them.
func newCharrefFixture(t *testing.T) *chatfix.Fixture {
	t.Helper()
	f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
	f.SQL(`INSERT INTO character (id, project_id, name) VALUES ('c1', ?, 'Mio')`, f.ProjectID)
	f.SQL(`INSERT INTO product (id, project_id, name) VALUES ('p1', ?, 'Serum')`, f.ProjectID)
	f.SQL(`INSERT INTO location (id, project_id, name, time) VALUES ('l1', ?, 'Street', 'dusk')`, f.ProjectID)
	f.Direct()
	return f
}

func sayWithImage(f *chatfix.Fixture, content string) service.Workspace {
	f.T.Helper()
	return f.Post(service.PostMessageInput{Content: content, Uploads: []service.Upload{{
		Name: "face.png", Size: int64(len(charrefPNG)),
		Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(charrefPNG)), nil },
	}}})
}

func faceOf(f *chatfix.Fixture, characterID string) string {
	f.T.Helper()
	var asset sql.NullString
	require.NoError(f.T, f.DB.Reader().QueryRow(`SELECT face_reference_asset_id FROM character WHERE id = ?`, characterID).Scan(&asset))
	return asset.String
}

func libraryCount(f *chatfix.Fixture) int {
	f.T.Helper()
	return countRows(f, `SELECT COUNT(*) FROM reference_item`)
}

func TestChat_AnAttachedImageBecomesTheFaceOfACharacterOnlyAfterTheAnswer(t *testing.T) {
	f := newCharrefFixture(t)

	ws := sayWithImage(f, "この画像を Mio の顔にして")

	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 1)
	body := chatfix.Body(cards[0])
	assert.Contains(t, body.Text, "この画像を「Mio」の顔（face）の参照にします")
	assert.Contains(t, body.Text, "この Project の Character だけが変わります")
	assert.Equal(t, 0, libraryCount(f), "nothing is registered before the answer")
	assert.Empty(t, faceOf(f, "c1"))

	ws = f.Answer(cards[0].ID, body.Options[0])

	assert.Contains(t, chatfix.LastText(ws), "「Mio」の顔（face）の参照を設定しました")
	require.Equal(t, 1, libraryCount(f))
	var name, kind, source, assetID string
	require.NoError(t, f.DB.Reader().QueryRow(`SELECT name, kind, source, asset_id FROM reference_item`).Scan(&name, &kind, &source, &assetID))
	assert.Equal(t, "Mio（顔）", name)
	assert.Equal(t, "character", kind)
	assert.Equal(t, "uploaded", source)
	assert.Equal(t, assetID, faceOf(f, "c1"), "the slot holds the Library entry's image, as when the screen assigns it")
	var assetType string
	require.NoError(t, f.DB.Reader().QueryRow(`SELECT type FROM asset WHERE id = ?`, assetID).Scan(&assetType))
	assert.Equal(t, "reference", assetType)
	view, err := f.Svc.World.View(context.Background(), f.ProjectID, "")
	require.NoError(t, err)
	assert.Equal(t, assetID, view.World.Characters[0].FaceReferenceAssetID)
}

func TestChat_TheBodySlotIsAssignedTheSameWay(t *testing.T) {
	f := newCharrefFixture(t)
	cards := chatfix.Cards(sayWithImage(f, "添付の写真を Mio の全身の参照にして").Messages)
	require.Len(t, cards, 1)

	f.Answer(cards[0].ID, "はい")

	var body sql.NullString
	require.NoError(t, f.DB.Reader().QueryRow(`SELECT body_reference_asset_id FROM character WHERE id = 'c1'`).Scan(&body))
	assert.NotEmpty(t, body.String)
	assert.Empty(t, faceOf(f, "c1"))
}

func TestChat_DecliningOrIgnoringTheReferenceCardChangesNothing(t *testing.T) {
	f := newCharrefFixture(t)
	cards := chatfix.Cards(sayWithImage(f, "この画像を Mio の顔にして").Messages)
	require.Len(t, cards, 1)

	ws := f.Answer(cards[0].ID, "やめる")

	assert.Contains(t, chatfix.LastText(ws), "何も変更していません")
	cards = chatfix.Cards(sayWithImage(f, "この画像を Mio の顔にして").Messages)
	require.NotEmpty(t, cards)
	f.Answer(cards[len(cards)-1].ID, "うーん、どうしよう")
	assert.Equal(t, 0, libraryCount(f))
	assert.Empty(t, faceOf(f, "c1"))
}

func TestChat_AReferenceRequestWithoutAnImageSaysItMustBeAttached(t *testing.T) {
	f := newCharrefFixture(t)

	ws := f.Say("この画像を Mio の顔にして", "")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "添付")
	assert.Equal(t, 0, libraryCount(f))
}

func TestChat_AnUnknownCharacterIsRefusedWithTheNamesOfTheProject(t *testing.T) {
	f := newCharrefFixture(t)

	ws := sayWithImage(f, "この画像をユウの顔にして")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "「ユウ」という Character は見つかりません")
	assert.Contains(t, chatfix.LastText(ws), "「Mio」")
	assert.Equal(t, 0, libraryCount(f))
}

func TestChat_ALockedFaceSlotIsRefusedAndNothingIsRegistered(t *testing.T) {
	f := newCharrefFixture(t)
	_, err := f.Svc.World.SetFieldLock(context.Background(), f.ProjectID, "", domain.TargetCharacter, "c1", "identity", domain.LockLocked)
	require.NoError(t, err)

	ws := sayWithImage(f, "この画像を Mio の顔にして")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "LOCK されているため変更できません")
	assert.Equal(t, 0, libraryCount(f))
}

// A LOCK set between the card and the answer refuses the answer; the Library entry the attempt made is taken out again.
func TestChat_ALockSetAfterTheCardRefusesTheAnswerAndLeavesNothingBehind(t *testing.T) {
	f := newCharrefFixture(t)
	cards := chatfix.Cards(sayWithImage(f, "この画像を Mio の顔にして").Messages)
	require.Len(t, cards, 1)
	_, err := f.Svc.World.SetFieldLock(context.Background(), f.ProjectID, "", domain.TargetCharacter, "c1", "identity", domain.LockLocked)
	require.NoError(t, err)

	ws := f.Answer(cards[0].ID, "はい")

	assert.Contains(t, chatfix.LastText(ws), "実行できませんでした")
	assert.Contains(t, chatfix.LastText(ws), "LOCK")
	assert.Equal(t, 0, libraryCount(f))
	assert.Empty(t, faceOf(f, "c1"))
}

func TestChat_SeveralMatchingCharactersAskWhichAndThePickIsCarriedOut(t *testing.T) {
	f := newCharrefFixture(t)
	f.SQL(`INSERT INTO character (id, project_id, name) VALUES ('c2', ?, 'Mio Jr')`, f.ProjectID)

	cards := chatfix.Cards(sayWithImage(f, "この画像を Mi の顔にして").Messages)

	require.Len(t, cards, 1)
	body := chatfix.Body(cards[0])
	assert.Contains(t, body.Text, "どの Character")
	require.Equal(t, []string{"Mio の顔にする", "Mio Jr の顔にする", "やめる"}, body.Options)
	assert.Equal(t, 0, libraryCount(f))

	f.Answer(cards[0].ID, "Mio Jr の顔にする")

	assert.NotEmpty(t, faceOf(f, "c2"))
	assert.Empty(t, faceOf(f, "c1"))
	assert.Equal(t, 1, libraryCount(f))
}

func TestChat_TheModelsReferenceOperationIsDroppedWhenTheMessageDoesNotAskForIt(t *testing.T) {
	f := newCharrefFixture(t)
	f.LLM.Interpreter = chatfix.OpsJSON(`{"type":"assign_character_reference","args":{"character":"Mio","slot":"face"}}`)

	ws := sayWithImage(f, "Mio の顔を笑顔にして")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Equal(t, 0, libraryCount(f))
	assert.Empty(t, faceOf(f, "c1"))
}

func TestChat_TheModelsReferenceOperationAsksTheSameCard(t *testing.T) {
	f := newCharrefFixture(t)
	f.LLM.Interpreter = chatfix.OpsJSON(`{"type":"assign_character_reference","args":{"character":"Mio","slot":"face","asset_id":"invented"}}`)

	cards := chatfix.Cards(sayWithImage(f, "この画像を Mio の顔の参照にして").Messages)

	require.Len(t, cards, 1)
	assert.Equal(t, 0, libraryCount(f))
}

// An image that is already in the thread (sent in an earlier message) is not an attachment of this one: the card is for the message that carries the image.
func TestChat_AnImageOfAnEarlierMessageIsNotUsed(t *testing.T) {
	f := newCharrefFixture(t)
	sayWithImage(f, "この雰囲気で")

	ws := f.Say("さっきの画像を Mio の顔にして", "")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "添付")
}
