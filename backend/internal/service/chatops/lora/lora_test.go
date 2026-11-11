package lora_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/lora"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
)

// fakeImporter is the importer of the model manager screen with the files it would stage given in advance (by name).
type fakeImporter struct {
	next      map[string]loraimport.Staged // by the name of the file sent
	refuse    map[string]error
	waiting   map[string]loraimport.Staged
	commits   []loraimport.Input
	discards  []string
	commitErr error
	read      map[string]string
}

func newImporter() *fakeImporter {
	return &fakeImporter{next: map[string]loraimport.Staged{}, refuse: map[string]error{}, waiting: map[string]loraimport.Staged{}, read: map[string]string{}}
}

func (f *fakeImporter) Stage(_ context.Context, name string, _ int64, r io.Reader) (loraimport.Staged, error) {
	body, _ := io.ReadAll(r)
	f.read[name] = string(body)
	if err := f.refuse[name]; err != nil {
		return loraimport.Staged{}, err
	}
	st, ok := f.next[name]
	if !ok {
		return loraimport.Staged{}, errors.New("unexpected file " + name)
	}
	f.waiting[st.Token] = st
	return st, nil
}

func (f *fakeImporter) Commit(_ context.Context, in loraimport.Input) (loraimport.Model, error) {
	f.commits = append(f.commits, in)
	st, ok := f.waiting[in.Token]
	if !ok {
		return loraimport.Model{}, chatops.Refuse("残っていません")
	}
	if f.commitErr != nil {
		return loraimport.Model{}, f.commitErr
	}
	delete(f.waiting, in.Token)
	name := st.Name
	if in.Name != "" {
		name = in.Name
	}
	m := loraimport.Model{}
	m.Name = name
	return m, nil
}

func (f *fakeImporter) Discard(token string) {
	f.discards = append(f.discards, token)
	delete(f.waiting, token)
}

func (f *fakeImporter) Staged(token string) (loraimport.Staged, bool) {
	st, ok := f.waiting[token]
	return st, ok
}

func file(name, body string) chatops.File {
	return chatops.File{Name: name, Size: int64(len(body)), Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }}
}

func staged(token, name string) loraimport.Staged {
	return loraimport.Staged{Token: token, OriginalName: name, Name: name, Size: 150 << 20, SHA256: "0123456789abcdef0123456789abcdef"}
}

var project = domain.Project{ID: "p1"}

// cardOf decodes the message the offer answers with: the card, the operation of its yes and of its "やめる".
func cardOf(t *testing.T, m domain.ChatMessage) (domain.CardBody, commandinterpreter.Operation, commandinterpreter.Operation) {
	t.Helper()
	assert.Equal(t, domain.KindQuestion, m.Kind)
	assert.Equal(t, domain.RefOperation, m.RefType)
	body := domain.DecodeCardBody(m.Content)
	require.Equal(t, []string{chatops.Yes, chatops.No}, body.Options)
	var yes, no commandinterpreter.Operation
	require.NoError(t, json.Unmarshal(body.OptionActions[0], &yes))
	require.NoError(t, json.Unmarshal(body.OptionActions[1], &no))
	return body, yes, no
}

func TestAccepts_TakesTheSafetensorsExtensionInAnyCase(t *testing.T) {
	o := lora.New(newImporter())
	for _, name := range []string{"style.safetensors", "Style.SafeTensors", "STYLE.SAFETENSORS", "dir/a b.safetensors", `C:\loras\a.safetensors`} {
		assert.True(t, o.Accepts(name), name)
	}
	for _, name := range []string{"a.png", "a.mp4", "a.ckpt", "safetensors", "a.safetensors.png", "a.safetensors.txt", ""} {
		assert.False(t, o.Accepts(name), name)
	}
}

func TestOffer_AFileGetsACardThatSaysWhatItIsAndChangesNothing(t *testing.T) {
	imp := newImporter()
	st := staged("tok1", "style.safetensors")
	st.Meta = lorafile.Metadata{BaseModels: []string{"sdxl"}, Title: "水彩の LoRA", TriggerWords: []string{"wcstyle", "soft"}, License: "CreativeML Open RAIL++-M"}
	imp.next["style.safetensors"] = st

	msgs, err := lora.New(imp).Offer(context.Background(), project, "これを入れて", []chatops.File{file("style.safetensors", "BYTES")})

	require.NoError(t, err)
	require.Len(t, msgs, 1)
	body, yes, no := cardOf(t, msgs[0])
	assert.Equal(t, "BYTES", imp.read["style.safetensors"], "the file is streamed to the importer")
	for _, want := range []string{"style.safetensors", "150 MiB", "0123456789abcdef", "SDXL", "水彩の LoRA", "wcstyle、soft", "CreativeML Open RAIL++-M", "推奨の強さ", "未設定", "全プロジェクトに効く（モデルの管理に追加され、単発生成・各 Project の生成で選べる）"} {
		assert.Contains(t, body.Text, want)
	}
	assert.NotContains(t, body.Text, "0123456789abcdef0", "only the short hash")
	assert.Equal(t, lora.OpAdd, yes.Type)
	assert.Equal(t, lora.OpDiscard, no.Type)
	var ya, na lora.Args
	require.NoError(t, yes.DecodeArgs(&ya))
	require.NoError(t, no.DecodeArgs(&na))
	assert.Equal(t, lora.Args{Token: "tok1"}, ya)
	assert.Equal(t, lora.Args{Token: "tok1"}, na)
	assert.Equal(t, "add_lora:tok1", msgs[0].RefID, "each file's card is answerable on its own")
	assert.Empty(t, imp.commits, "nothing is added before the user accepts")
	assert.Empty(t, imp.discards)
	assert.Contains(t, imp.waiting, "tok1", "the file waits for the answer")
}

func TestOffer_ACardSaysWhatTheFileDoesNotSay(t *testing.T) {
	imp := newImporter()
	st := staged("tok1", "plain.safetensors")
	st.Meta = lorafile.Metadata{BaseRaw: []string{"my-own-base"}}
	imp.next["plain.safetensors"] = st

	msgs, err := lora.New(imp).Offer(context.Background(), project, "", []chatops.File{file("plain.safetensors", "x")})

	require.NoError(t, err)
	body, _, _ := cardOf(t, msgs[0])
	assert.Contains(t, body.Text, "対応するベースモデル: 未設定（ファイルには「my-own-base」とあります")
	assert.Contains(t, body.Text, "トリガー語: 未設定")
	assert.Contains(t, body.Text, "ライセンス: 不明（ファイルには書かれていません）")
	assert.NotContains(t, body.Text, "ファイルの題名")
}

func TestOffer_ARenamedFileSaysSo(t *testing.T) {
	imp := newImporter()
	st := staged("tok1", "my style (v2).safetensors")
	st.Name, st.Renamed = "my_style__v2_.safetensors", true
	imp.next["my style (v2).safetensors"] = st

	msgs, err := lora.New(imp).Offer(context.Background(), project, "", []chatops.File{file("my style (v2).safetensors", "x")})

	require.NoError(t, err)
	body, _, _ := cardOf(t, msgs[0])
	assert.Contains(t, body.Text, "ファイル名: my_style__v2_.safetensors（使えない文字を _ に置き換えました。元の名前: my style (v2).safetensors）")
}

func TestOffer_ATakenNameIsSaidAndTheFreeNameIsWhatTheYesAdds(t *testing.T) {
	imp := newImporter()
	st := staged("tok1", "style.safetensors")
	st.Conflict = &loraimport.Conflict{Name: "style.safetensors", Suggest: "style-0123abcd.safetensors"}
	imp.next["style.safetensors"] = st

	msgs, err := lora.New(imp).Offer(context.Background(), project, "", []chatops.File{file("style.safetensors", "x")})

	require.NoError(t, err)
	body, yes, _ := cardOf(t, msgs[0])
	assert.Contains(t, body.Text, "ファイル名: style-0123abcd.safetensors")
	assert.Contains(t, body.Text, "名前の重複: 「style.safetensors」という名前のファイルがすでにあり、上書きはしません。style-0123abcd.safetensors という名前で追加します")
	var a lora.Args
	require.NoError(t, yes.DecodeArgs(&a))
	assert.Equal(t, lora.Args{Token: "tok1", Name: "style-0123abcd.safetensors"}, a)

	_, err = lora.New(imp).Apply(context.Background(), project, yes)
	require.NoError(t, err)
	require.Len(t, imp.commits, 1)
	assert.Equal(t, "style-0123abcd.safetensors", imp.commits[0].Name)
}

func TestOffer_AFileThatIsThereAlreadyGetsNoCardAndIsDiscarded(t *testing.T) {
	imp := newImporter()
	st := staged("tok1", "style.safetensors")
	st.Conflict = &loraimport.Conflict{Name: "style.safetensors", Same: true, Suggest: "x.safetensors"}
	imp.next["style.safetensors"] = st

	msgs, err := lora.New(imp).Offer(context.Background(), project, "", []chatops.File{file("style.safetensors", "x")})

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Empty(t, msgs)
	assert.Contains(t, ref.Message, "すでに「style.safetensors」")
	assert.Equal(t, []string{"tok1"}, imp.discards)
	assert.Empty(t, imp.commits)
	assert.Empty(t, imp.waiting)
}

func TestOffer_ARefusedFileIsAReasonNotACard(t *testing.T) {
	for _, reason := range []string{
		"LoRA のファイルではありません", "ファイルが大きすぎます（5 GB。上限 4 GB）", "空き容量が足りません", "確認待ちのファイルが 3 個あります。先にそれを追加するか取り消してください",
		"同梱の ComfyUI が使えない環境のため、LoRA を追加できません",
	} {
		imp := newImporter()
		imp.refuse["bad.safetensors"] = chatops.Refuse("%s", reason)

		msgs, err := lora.New(imp).Offer(context.Background(), project, "", []chatops.File{file("bad.safetensors", "x")})

		var ref *chatops.Refusal
		require.ErrorAs(t, err, &ref, reason)
		assert.Equal(t, reason, ref.Message, "told as the screen tells it")
		assert.Empty(t, msgs)
		assert.Empty(t, imp.commits)
	}
}

func TestOffer_SeveralFilesGetACardEachAndARefusedOneIsTold(t *testing.T) {
	imp := newImporter()
	imp.next["a.safetensors"], imp.next["b.safetensors"] = staged("tokA", "a.safetensors"), staged("tokB", "b.safetensors")
	imp.refuse["bad.safetensors"] = chatops.Refuse("LoRA のファイルではありません")
	files := []chatops.File{file("a.safetensors", "1"), file("bad.safetensors", "2"), file("b.safetensors", "3")}

	msgs, err := lora.New(imp).Offer(context.Background(), project, "", files)

	require.NoError(t, err)
	require.Len(t, msgs, 3)
	assert.Equal(t, "add_lora:tokA", msgs[0].RefID)
	assert.Equal(t, "add_lora:tokB", msgs[1].RefID)
	assert.Equal(t, domain.KindText, msgs[2].Kind)
	assert.Equal(t, "取り込めませんでした: bad.safetensors: LoRA のファイルではありません", msgs[2].Content)
	assert.Empty(t, imp.commits)
}

func TestOffer_EverySeveralFilesRefusedIsOneRefusalThatNamesThem(t *testing.T) {
	imp := newImporter()
	imp.refuse["a.safetensors"], imp.refuse["b.safetensors"] = chatops.Refuse("大きすぎます"), chatops.Refuse("LoRA ではありません")

	_, err := lora.New(imp).Offer(context.Background(), project, "", []chatops.File{file("a.safetensors", "1"), file("b.safetensors", "2")})

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Equal(t, "a.safetensors: 大きすぎます\nb.safetensors: LoRA ではありません", ref.Message)
}

func TestOffer_AFaultDiscardsWhatThisMessageStaged(t *testing.T) {
	imp := newImporter()
	imp.next["a.safetensors"] = staged("tokA", "a.safetensors")
	fault := errors.New("disk exploded")
	files := []chatops.File{file("a.safetensors", "1"), {Name: "b.safetensors", Open: func() (io.ReadCloser, error) { return nil, fault }}}

	msgs, err := lora.New(imp).Offer(context.Background(), project, "", files)

	require.ErrorIs(t, err, fault)
	assert.Empty(t, msgs)
	assert.Equal(t, []string{"tokA"}, imp.discards)
}
