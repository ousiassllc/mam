package chattest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// assignServices are the real services of the model assignments, over files of a temporary directory (no network: the LLM model lists come from mam's candidates).
type assignServices struct {
	models              *modelsettings.Service
	llm                 *llmassign.Service
	modelsPath, llmPath string
}

func newAssignServices(t *testing.T) assignServices {
	t.Helper()
	dir := t.TempDir()
	s := assignServices{modelsPath: filepath.Join(dir, "models.json"), llmPath: filepath.Join(dir, "llm.json")}
	s.models = modelsettings.New(s.modelsPath, false)
	s.models.SetAvailability(func(id string) (modelfilter.Availability, bool) {
		if id == string(domain.ModelKling4) {
			return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キー未設定", "/settings/api-keys"), true
		}
		return modelfilter.Availability{}, false
	})
	s.llm = llmassign.New(s.llmPath, false)
	return s
}

func (s assignServices) settings() chatwire.Settings {
	return chatwire.Settings{Models: s.models, LLM: s.llm, LLMModels: llmmodels.New(llmmodels.Options{})}
}

// assignFixture is the Project with the chat operations wired to these services.
func assignFixture(t *testing.T, opts ...chatfix.Option) (*chatfix.Fixture, assignServices) {
	t.Helper()
	s := newAssignServices(t)
	return chatfix.New(t, append([]chatfix.Option{chatfix.Wired(s.settings())}, opts...)...), s
}

// assignFileObject is the object under key of a settings file; nil when the file or the key is not there.
func assignFileObject(t *testing.T, path, key string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &top))
	var obj map[string]json.RawMessage
	if raw, ok := top[key]; ok {
		require.NoError(t, json.Unmarshal(raw, &obj))
	}
	return obj
}

// "Quality MAX の動画モデルを kling-4.0 に" in the chat: a card that shows the route before and after, with the warning and that it is global; nothing is saved until the user accepts;
// then the same models.json as the screen writes, with only that Quality Policy changed.
func TestChat_ModelAssignmentAsksFirstAndSavesOnlyTheNamedRouteAfterAccepting(t *testing.T) {
	for name, prepare := range map[string]func(f *chatfix.Fixture){
		"DIRECTOR MODE": (*chatfix.Fixture).Direct,
		"AUTO MODE":     (*chatfix.Fixture).Auto,
	} {
		t.Run(name, func(t *testing.T) {
			f, s := assignFixture(t)
			prepare(f)
			before, err := s.models.View()
			require.NoError(t, err)

			ws := f.SayOps("Quality MAX の動画モデルを kling-4.0 の 1080p に", `{"type":"set_model_assignment","args":{"target":"max_quality","model":"kling-4.0","resolution":"1080p"}}`)

			cards := chatfix.Cards(ws.Messages)
			require.Len(t, cards, 1)
			assert.Equal(t, "set_model_assignment", cards[0].RefID)
			body := chatfix.Body(cards[0])
			assert.Contains(t, body.Text, "Quality Policy「MAX QUALITY」の動画モデル（主モデル）")
			assert.Contains(t, body.Text, "いま: 主 runway-gen-4.5（720p）")
			assert.Contains(t, body.Text, "変更後: 主 kling-4.0（1080p）")
			assert.Contains(t, body.Text, "kling-4.0 は今使えません: API キー未設定")
			assert.Contains(t, body.Text, "全プロジェクトに効きます")
			assert.Contains(t, body.Text, "すぐに反映されます")
			_, statErr := os.Stat(s.modelsPath)
			assert.True(t, os.IsNotExist(statErr), "nothing is written before the user accepts")

			ws = f.Answer(cards[0].ID, body.Options[0])

			assert.Contains(t, chatfix.LastText(ws), "動画モデル（主モデル）を変えました")
			after, err := s.models.View()
			require.NoError(t, err)
			want := before.Policies[domain.QualityMaxQuality]
			want.Primary.Model, want.Primary.Resolution = domain.ModelKling4, "1080p"
			assert.Equal(t, want, after.Policies[domain.QualityMaxQuality], "the fallback and the Takes of the policy stay")
			for _, q := range []domain.QualityPolicy{domain.QualityFast, domain.QualityBalanced} {
				assert.Equal(t, before.Policies[q], after.Policies[q])
			}
			assert.Equal(t, before.Images, after.Images)
			policies := assignFileObject(t, s.modelsPath, "policies")
			assert.Len(t, policies, 1, "the file holds only what differs from the defaults")
			assert.Contains(t, policies, string(domain.QualityMaxQuality))
		})
	}
}

func TestChat_ModelAssignmentDeclinedOrIgnoredChangesNothing(t *testing.T) {
	f, s := assignFixture(t)
	f.Direct()
	card := chatfix.Cards(f.SayOps("生成の画像モデルの 新規参照 を flux にして", `{"type":"set_model_assignment","args":{"target":"new_reference","model":"flux"}}`).Messages)[0]
	assert.Contains(t, chatfix.Body(card).Text, "変更後: 主 flux-2")

	ws := f.Answer(card.ID, "やめる")

	assert.Equal(t, "やめました。何も変更していません。", chatfix.LastText(ws))
	_, err := os.Stat(s.modelsPath)
	assert.True(t, os.IsNotExist(err))

	// a reply that is no answer is a new message: the card is not accepted by it
	card = chatfix.Cards(f.Say("生成の画像モデルの 新規参照 を flux にして", "").Messages)[0]
	f.LLM.Interpreter = chatfix.OpsJSON()
	f.Say("カメラを固定して", card.ID)
	_, err = os.Stat(s.modelsPath)
	assert.True(t, os.IsNotExist(err), "an unrelated reply saves nothing")

	// the model's word alone is not enough: a message that does not ask for it
	assert.Empty(t, chatfix.Cards(f.SayOps("カメラを固定して", `{"type":"set_model_assignment","args":{"target":"new_reference","model":"flux"}}`).Messages))
	_, err = os.Stat(s.modelsPath)
	assert.True(t, os.IsNotExist(err))
}

func TestChat_ModelAssignmentThatIsAlreadyTheSameOrNotOfferedPostsNoCard(t *testing.T) {
	f, s := assignFixture(t)
	f.Direct()

	ws := f.SayOps("Quality MAX の動画モデルを runway-gen-4.5 にして", `{"type":"set_model_assignment","args":{"target":"max_quality","model":"runway-gen-4.5","resolution":"720p","takes":4}}`)
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "すでにその内容です")

	ws = f.SayOps("Quality MAX の動画モデルを midjourney にして", `{"type":"set_model_assignment","args":{"target":"max_quality","model":"midjourney"}}`)
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "「midjourney」という動画モデルは選べません")
	_, err := os.Stat(s.modelsPath)
	assert.True(t, os.IsNotExist(err))
}

// "Producer の代替を … にして" with no LLM set up (the model-free reading): a card first, then llm.json as the screen writes it, with only that Agent's route changed.
func TestChat_LLMAssignmentAsksFirstAndSavesOnlyTheNamedAgentAfterAccepting(t *testing.T) {
	f, s := assignFixture(t, chatfix.WithoutLLM())
	candidate := llm.CandidateModels(llm.ProviderOpenAI)[0]
	before := s.llm.View()

	card := assignCard(t, f, "Producer の代替を "+candidate+" にして")

	assert.Equal(t, "set_llm_assignment", card.RefID)
	body := chatfix.Body(card)
	assert.Contains(t, body.Text, "Producer（コンセプト） の代替の LLM を変えますね？")
	assert.Contains(t, body.Text, "いま: プライマリ "+before.Entries[1].Route.Primary.Provider)
	assert.Contains(t, body.Text, "代替 openai / "+candidate)
	assert.Contains(t, body.Text, "全プロジェクトに効きます")
	assert.Contains(t, body.Text, "注意: openai は今使えません: API キーが登録されていません", "no key is saved: a warning, not a refusal")
	assert.NotContains(t, body.Text, "sk-")
	_, statErr := os.Stat(s.llmPath)
	assert.True(t, os.IsNotExist(statErr), "nothing is written before the user accepts")

	ws := f.Answer(card.ID, "やめる")
	assert.Equal(t, "やめました。何も変更していません。", chatfix.LastText(ws))
	assert.Equal(t, before.Entries, s.llm.View().Entries)

	card = assignCard(t, f, "Producer の代替を "+candidate+" にして")
	ws = f.Answer(card.ID, "はい")

	assert.Contains(t, chatfix.LastText(ws), "代替を openai / "+candidate+" にしました")
	after := s.llm.View()
	require.Len(t, after.Entries, len(before.Entries))
	for i, e := range after.Entries {
		if e.Agent != "producer" {
			assert.Equal(t, before.Entries[i].Route, e.Route, e.Agent)
			continue
		}
		assert.Equal(t, before.Entries[i].Route.Primary, e.Route.Primary, "the other half of the route stays")
		assert.Equal(t, llmassign.Target{Provider: "openai", Model: candidate}, e.Route.Fallback)
	}
	routes := assignFileObject(t, s.llmPath, "routes")
	assert.Len(t, routes, 1, "the file holds only the Agent that differs from the defaults")
	assert.Contains(t, routes, "producer")
}

func TestChat_LLMAssignmentRefusedByTheServiceIsToldAndNothingIsSaved(t *testing.T) {
	pinned := chatwire.Settings{
		LLM:       llmassign.New(filepath.Join(t.TempDir(), "llm.json"), true),
		LLMModels: llmmodels.New(llmmodels.Options{}),
		Models:    modelsettings.New(filepath.Join(t.TempDir(), "models.json"), true),
	}
	f := chatfix.New(t, chatfix.WithoutLLM(), chatfix.Wired(pinned))

	ws := f.Say("Director の LLM を claude-opus-4-5 にして", "")
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "--llm-config")
}

// The model's word alone does not change the LLM assignment; when the message does ask for it, the model's reading (not only the model-free one) gives the card.
func TestChat_LLMAssignmentFollowsTheModelsReadingOnlyWhenTheMessageAsksForIt(t *testing.T) {
	f, s := assignFixture(t)
	f.Direct()
	current := s.llm.View().Entries[3].Route.Primary.Model // the Director
	var candidate string
	for _, id := range llm.CandidateModels(llm.ProviderAnthropic) {
		if id != current {
			candidate = id
			break
		}
	}
	require.NotEmpty(t, candidate)
	f.LLM.Interpreter = chatfix.OpsJSON(`{"type":"set_llm_assignment","args":{"agent":"director","role":"primary","provider":"anthropic","model":"` + candidate + `"}}`)

	assert.Empty(t, chatfix.Cards(f.Say("カメラを固定して", "").Messages), "a message that does not ask for it drops the model's operation")

	cards := chatfix.Cards(f.Say("Director の LLM を "+candidate+" にして", "").Messages)
	require.Len(t, cards, 1)
	body := chatfix.Body(cards[0])
	assert.Contains(t, body.Text, "Director（Scene / Shot の計画） のプライマリの LLM を変えますね？")
	assert.Contains(t, body.Text, "全プロジェクトに効きます")
	_, err := os.Stat(s.llmPath)
	assert.True(t, os.IsNotExist(err), "nothing is written before the user accepts")
}

// assignCard sends the message and returns the one card it gives.
func assignCard(t *testing.T, f *chatfix.Fixture, content string) domain.ThreadMessage {
	t.Helper()
	cards := chatfix.Cards(f.Say(content, "").Messages)
	require.Len(t, cards, 1)
	return cards[0]
}
