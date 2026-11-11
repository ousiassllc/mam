package chattest

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/lora"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
)

// loraFake is the LoRA importer of the model manager with the files it keeps in memory: what was staged, committed and discarded.
type loraFake struct {
	mu       sync.Mutex
	waiting  map[string]loraimport.Staged
	staged   []string // names, in the order they arrived
	commits  []loraimport.Input
	discards []string
	refuse   error
	meta     lorafile.Metadata
}

func (f *loraFake) Stage(_ context.Context, name string, _ int64, r io.Reader) (loraimport.Staged, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := io.Copy(io.Discard, r); err != nil {
		return loraimport.Staged{}, err
	}
	if f.refuse != nil {
		return loraimport.Staged{}, f.refuse
	}
	f.staged = append(f.staged, name)
	token := "tok-" + name
	st := loraimport.Staged{Token: token, OriginalName: name, Name: name, Size: 64 << 20, SHA256: strings.Repeat("ab", 32), Meta: f.meta}
	if f.waiting == nil {
		f.waiting = map[string]loraimport.Staged{}
	}
	f.waiting[token] = st
	return st, nil
}

func (f *loraFake) Commit(_ context.Context, in loraimport.Input) (loraimport.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, in)
	st, ok := f.waiting[in.Token]
	if !ok {
		return loraimport.Model{}, chatops.Refuse("残っていません")
	}
	delete(f.waiting, in.Token)
	m := loraimport.Model{}
	m.Name = st.Name
	return m, nil
}

func (f *loraFake) Discard(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discards = append(f.discards, token)
	delete(f.waiting, token)
}

func (f *loraFake) Staged(token string) (loraimport.Staged, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.waiting[token]
	return st, ok
}

func (f *loraFake) snapshot() (commits []loraimport.Input, discards []string, waiting int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]loraimport.Input(nil), f.commits...), append([]string(nil), f.discards...), len(f.waiting)
}

type loraFixture struct {
	*chatfix.Fixture
	imp *loraFake
}

// loraPNG is the head of a PNG: enough for the chat to take the file as an image.
var loraPNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

func loraUpload(name string, data []byte) service.Upload {
	return service.Upload{Name: name, Size: int64(len(data)), Open: func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}}
}

// newLoRAFixture is a service whose chat takes .safetensors files through the offer of the LoRA operation, backed by a fake importer.
func newLoRAFixture(t *testing.T) loraFixture {
	t.Helper()
	f := chatfix.New(t)
	imp := &loraFake{meta: lorafile.Metadata{BaseModels: []string{"sdxl"}, TriggerWords: []string{"wcstyle"}}}
	offer := lora.New(imp)
	f.Svc.ChatOps().Files(offer)
	f.Svc.ChatOps().Handle(lora.OpAdd, offer)
	f.Svc.ChatOps().Handle(lora.OpDiscard, offer)
	return loraFixture{Fixture: f, imp: imp}
}

func (f loraFixture) attach(content string, names ...string) service.Workspace {
	f.T.Helper()
	uploads := make([]service.Upload, 0, len(names))
	for _, n := range names {
		uploads = append(uploads, loraUpload(n, []byte("not really a LoRA")))
	}
	return f.Post(service.PostMessageInput{Content: content, Uploads: uploads})
}

func (f loraFixture) requireNoAsset() {
	f.T.Helper()
	view, err := f.Svc.Projects.Brief(context.Background(), f.ProjectID)
	require.NoError(f.T, err)
	assert.Empty(f.T, view.Assets, "the LoRA is not an image or video of the Brief")
}

// A message with a .safetensors file is not refused as an unsupported attachment: it is handed to the LoRA importer, the thread gets the confirmation card, and the file is
// not an attachment of the Brief. Nothing is added before the user answers.
func TestChat_ASafetensorsFileGetsAConfirmationCardAndChangesNothing(t *testing.T) {
	f := newLoRAFixture(t)

	ws := f.attach("この LoRA を使いたい", "style.safetensors")

	require.Len(t, ws.Messages, 2, "the user's message and the card")
	assert.Equal(t, "この LoRA を使いたい", ws.Messages[0].Content)
	assert.Empty(t, ws.Messages[0].Attachments, "no asset of the Brief")
	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 1)
	body := chatfix.Body(cards[0])
	assert.Contains(t, body.Text, "style.safetensors")
	assert.Contains(t, body.Text, "SDXL")
	assert.Contains(t, body.Text, "全プロジェクトに効く（モデルの管理に追加され、単発生成・各 Project の生成で選べる）")
	assert.Equal(t, []string{chatops.Yes, chatops.No}, body.Options)
	assert.True(t, cards[0].Open)
	commits, discards, waiting := f.imp.snapshot()
	assert.Empty(t, commits)
	assert.Empty(t, discards)
	assert.Equal(t, 1, waiting, "the file waits for the answer")
	f.requireNoAsset()
}

func TestChat_ASafetensorsFileWithoutTextIsNamedInTheMessage(t *testing.T) {
	f := newLoRAFixture(t)

	ws := f.attach("", "style.safetensors")

	assert.Equal(t, "添付: style.safetensors", ws.Messages[0].Content)
	require.Len(t, chatfix.Cards(ws.Messages), 1)
}

func TestChat_AcceptingTheCardAddsTheFileOnceThroughTheImporter(t *testing.T) {
	f := newLoRAFixture(t)
	card := chatfix.Cards(f.attach("", "style.safetensors").Messages)[0]

	ws := f.Answer(card.ID, chatops.Yes)

	assert.Contains(t, chatfix.LastText(ws), "「style.safetensors」を LoRA として追加しました")
	assert.Contains(t, chatfix.LastText(ws), "全プロジェクト")
	commits, discards, waiting := f.imp.snapshot()
	require.Len(t, commits, 1)
	assert.Equal(t, loraimport.Input{Token: "tok-style.safetensors", Meta: loraimport.Meta{BaseModels: []string{"sdxl"}, TriggerWords: "wcstyle"}}, commits[0])
	assert.Empty(t, discards)
	assert.Zero(t, waiting)
	require.Len(t, ws.Updated, 1)
	assert.False(t, ws.Updated[0].Open, "the card is closed")

	_, err := f.Svc.Chat.PostMessage(context.Background(), f.ProjectID, service.PostMessageInput{Content: chatops.Yes, ReplyToMessageID: card.ID})
	require.ErrorIs(t, err, service.ErrConflict, "a card is answered once")
	commits, _, _ = f.imp.snapshot()
	assert.Len(t, commits, 1)
}

func TestChat_DecliningTheCardThrowsTheFileAway(t *testing.T) {
	for name, reply := range map[string]string{"button": chatops.No, "word": "キャンセル"} {
		t.Run(name, func(t *testing.T) {
			f := newLoRAFixture(t)
			card := chatfix.Cards(f.attach("", "style.safetensors").Messages)[0]

			ws := f.Answer(card.ID, reply)

			commits, discards, waiting := f.imp.snapshot()
			assert.Empty(t, commits, "nothing is added")
			assert.Equal(t, []string{"tok-style.safetensors"}, discards, "the staged file is removed from the imports folder")
			assert.Zero(t, waiting)
			assert.Contains(t, chatfix.LastText(ws), "何も追加していません")
			require.Len(t, ws.Updated, 1)
			assert.False(t, ws.Updated[0].Open)
		})
	}
}

// A reply that is neither a yes nor a no is a new message: it adds nothing.
func TestChat_AnUnrelatedReplyAddsNothing(t *testing.T) {
	f := newLoRAFixture(t)
	card := chatfix.Cards(f.attach("", "style.safetensors").Messages)[0]

	f.Answer(card.ID, "ところで、予算はどのくらい？")

	commits, _, _ := f.imp.snapshot()
	assert.Empty(t, commits)
}

func TestChat_EachFileOfAMessageHasACardOfItsOwn(t *testing.T) {
	f := newLoRAFixture(t)

	ws := f.attach("", "a.safetensors", "b.safetensors")

	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 2)
	assert.Contains(t, chatfix.Body(cards[0]).Text, "a.safetensors")
	assert.Contains(t, chatfix.Body(cards[1]).Text, "b.safetensors")
	assert.True(t, cards[0].Open)
	assert.True(t, cards[1].Open, "the first card is not replaced by the second")

	f.Answer(cards[0].ID, chatops.No)
	f.Answer(cards[1].ID, chatops.Yes)

	commits, discards, _ := f.imp.snapshot()
	require.Len(t, commits, 1)
	assert.Equal(t, "tok-b.safetensors", commits[0].Token)
	assert.Equal(t, []string{"tok-a.safetensors"}, discards)
}

func TestChat_ARefusedFileIsToldWithoutACard(t *testing.T) {
	f := newLoRAFixture(t)
	f.imp.refuse = chatops.Refuse("LoRA のファイルではありません（テンソルの名前が LoRA のものではありません）")

	ws := f.attach("これ", "style.safetensors")

	require.Len(t, ws.Messages, 2)
	assert.Equal(t, "取り込めませんでした: LoRA のファイルではありません（テンソルの名前が LoRA のものではありません）", ws.Messages[1].Content)
	assert.Empty(t, chatfix.Cards(ws.Messages))
	commits, _, waiting := f.imp.snapshot()
	assert.Empty(t, commits)
	assert.Zero(t, waiting)
	f.requireNoAsset()
}

// An image sent along with the LoRA still goes to the Brief, and an image alone is not handed to the importer.
func TestChat_ImagesStayAttachmentsOfTheBrief(t *testing.T) {
	f := newLoRAFixture(t)

	ws := f.Post(service.PostMessageInput{Content: "雰囲気はこれ", Uploads: []service.Upload{loraUpload("look.png", loraPNG), loraUpload("style.safetensors", []byte("x"))}})

	require.Len(t, ws.Messages[0].Attachments, 1, "the image is an attachment of the message")
	assert.Equal(t, domain.AssetImage, ws.Messages[0].Attachments[0].Type)
	require.Len(t, chatfix.Cards(ws.Messages), 1)

	before, _, _ := f.imp.snapshot()
	f.Post(service.PostMessageInput{Content: "もう一枚", Uploads: []service.Upload{loraUpload("look2.png", loraPNG)}})
	after, _, _ := f.imp.snapshot()
	assert.Equal(t, before, after)
	assert.Len(t, f.imp.staged, 1, "only the .safetensors file reached the importer")
}
