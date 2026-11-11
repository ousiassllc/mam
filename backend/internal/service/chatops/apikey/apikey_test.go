package apikey_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/apikey"
)

// detected is an operation the message plainly asks for, with its type.
type detected struct {
	Type commandinterpreter.OpType
	Args apikey.Args
}

func detect(t *testing.T, message string) []detected {
	t.Helper()
	var out []detected
	for _, op := range commandinterpreter.Detected(message) {
		d := detected{Type: op.Type}
		require.NoError(t, op.DecodeArgs(&d.Args))
		out = append(out, d)
	}
	return out
}

func TestDetect_ReadsTheKeyRequestWithoutAModel(t *testing.T) {
	for message, provider := range map[string]string{
		"OpenAI の API キーを設定したい": "openai",
		"Claude のキーを入れ替えたい":     "anthropic",
		"Gemini のAPIキーを登録して":    "google",
		"Kling 4.0 の API キーを設定": "kling4",
		"Kling の API キーを設定":     "kling",
		"fal.ai の API キーを入れたい":  "fal",
		"API キーを設定したい":          "",
	} {
		got := detect(t, message)
		require.Len(t, got, 1, message)
		assert.Equal(t, apikey.OpSetCredential, got[0].Type, message)
		assert.Equal(t, provider, got[0].Args.Provider, message)
	}
}

func TestDetect_DoesNotMistakeOtherMessagesForAKeyRequest(t *testing.T) {
	for _, message := range []string{
		"API キーは持っている",
		"ショートカットキーを変更したい",
		"カメラを固定して",
		"予算を $30 に変えて",
		"swan のシーンを足して", // "wan" inside a word
	} {
		assert.Empty(t, detect(t, message), message)
	}
}

func TestDetect_AQuestionAboutAKeyIsNotAKeyRequest(t *testing.T) {
	for _, message := range []string{
		"API キーの設定画面はどこ？",
		"OpenAI の API キーを教えて",
		"OpenAI の API キーを教えてください？",
		"Claude のキーの設定はどこですか？",
		"API キーを設定するにはどうしたらいい？",
	} {
		assert.Empty(t, detect(t, message), message)
		assert.False(t, apikey.AsksKey(message), message)
	}
	for _, message := range []string{"API キーを設定したい", "OpenAI の API キーを入力します", "API キーを設定してくれる？"} {
		assert.True(t, apikey.AsksKey(message), message)
	}
}

func TestDetect_ReadsTheVideoProvider(t *testing.T) {
	got := detect(t, "Kling は OpenRouter 経由で使いたい")
	require.Len(t, got, 1)
	assert.Equal(t, apikey.OpSetVideoProvider, got[0].Type)
	assert.Equal(t, "openrouter", got[0].Args.Provider)
	got = detect(t, "Kling は公式に戻して")
	require.Len(t, got, 1)
	assert.Equal(t, apikey.OpSetVideoProvider, got[0].Type)
	assert.Equal(t, "official", got[0].Args.Provider)
	got = detect(t, "OpenRouter の API キーを設定したい")
	require.Len(t, got, 1, "naming the provider of a key is a key request, not a choice of the video provider")
	assert.Equal(t, apikey.OpSetCredential, got[0].Type)
}

func TestClean_DropsAProviderTheModelInvented(t *testing.T) {
	op, err := commandinterpreter.NewOperation(apikey.OpSetCredential, apikey.Args{Provider: "mystery"})
	require.NoError(t, err)

	cleaned, ok := commandinterpreter.Normalize(op)

	require.True(t, ok)
	var a apikey.Args
	require.NoError(t, cleaned.DecodeArgs(&a))
	assert.Empty(t, a.Provider, "the chat asks which instead of guessing")
}

func TestPrompt_DescribesBothOperationsAndNeverAsksForAKey(t *testing.T) {
	for _, typ := range []commandinterpreter.OpType{apikey.OpSetCredential, apikey.OpSetVideoProvider} {
		spec, ok := commandinterpreter.SpecOf(typ)
		require.True(t, ok)
		assert.True(t, spec.Confirm && spec.Global && spec.AnyMode)
		assert.NotContains(t, spec.Example, "api_key")
	}
}

type fakeKeys struct {
	configured map[string]bool
	saved      map[string]string
	saveErr    error
}

func (f *fakeKeys) deps() apikey.KeyDeps {
	return apikey.KeyDeps{
		Configured: func(p string) (bool, error) { return f.configured[p], nil },
		Save: func(p, key string) error {
			if f.saveErr != nil {
				return f.saveErr
			}
			f.saved[p] = key
			return nil
		},
	}
}

func TestKey_TheCardNamesTheProviderAndCarriesNoKey(t *testing.T) {
	f := &fakeKeys{configured: map[string]bool{"openai": true}, saved: map[string]string{}}
	op, _ := commandinterpreter.NewOperation(apikey.OpSetCredential, apikey.Args{Provider: "openai"})

	card, err := apikey.NewKey(f.deps()).Question(context.Background(), domain.Project{}, op)

	require.NoError(t, err)
	require.NotNil(t, card.Secret)
	assert.Equal(t, "openai", card.Secret.Provider)
	assert.Contains(t, card.Text, "置き換えます")
	assert.Contains(t, card.Text, "全プロジェクト")
	assert.Empty(t, f.saved, "asking stores nothing")
	body, err := chatops.Body(op, card)
	require.NoError(t, err)
	assert.Equal(t, []string{chatops.No}, body.Options)
}

func TestKey_ApplyNeverCarriesAKey(t *testing.T) {
	f := &fakeKeys{saved: map[string]string{}}
	op, _ := commandinterpreter.NewOperation(apikey.OpSetCredential, apikey.Args{Provider: "openai"})

	_, err := apikey.NewKey(f.deps()).Apply(context.Background(), domain.Project{}, op)

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Empty(t, f.saved)
}

func TestKey_SubmitStoresAndTheAnswerHoldsNoKey(t *testing.T) {
	f := &fakeKeys{saved: map[string]string{}}
	field := domain.SecretField{Provider: "openai", Label: "OpenAI"}

	res, err := apikey.NewKey(f.deps()).Submit(context.Background(), domain.Project{}, field, "sk-secret-value-0123456789")

	require.NoError(t, err)
	assert.Equal(t, "sk-secret-value-0123456789", f.saved["openai"])
	assert.NotContains(t, res.Text, "sk-secret")
	assert.Contains(t, res.Text, "全プロジェクト")
}

func TestKey_SubmitPassesAnErrorOnWithoutTheKey(t *testing.T) {
	f := &fakeKeys{saved: map[string]string{}, saveErr: errors.New("keychain locked")}

	_, err := apikey.NewKey(f.deps()).Submit(context.Background(), domain.Project{}, domain.SecretField{Provider: "openai", Label: "OpenAI"}, "sk-secret-value-0123456789")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sk-secret")
}

type fakeProvider struct {
	view     apikey.ProviderView
	keys     map[string]bool
	saved    string
	saveErr  error
	savedSet bool
}

func (f *fakeProvider) deps() apikey.ProviderDeps {
	return apikey.ProviderDeps{
		View:       func() (apikey.ProviderView, error) { return f.view, nil },
		Configured: func(p string) (bool, error) { return f.keys[p], nil },
		Save:       func(p string) error { f.saved, f.savedSet = p, true; return f.saveErr },
	}
}

func providerOp(t *testing.T, provider string) commandinterpreter.Operation {
	t.Helper()
	op, err := commandinterpreter.NewOperation(apikey.OpSetVideoProvider, apikey.Args{Provider: provider})
	require.NoError(t, err)
	return op
}

func TestProvider_AskFirstThenSaveAndAskForTheMissingKey(t *testing.T) {
	f := &fakeProvider{view: apikey.ProviderView{Selected: "official"}, keys: map[string]bool{}}
	h := apikey.NewProvider(f.deps())

	card, err := h.Question(context.Background(), domain.Project{}, providerOp(t, "fal"))
	require.NoError(t, err)
	assert.Contains(t, card.Text, "fal.ai")
	assert.Contains(t, card.Text, "全プロジェクト")
	assert.False(t, f.savedSet, "nothing is saved by asking")

	res, err := h.Apply(context.Background(), domain.Project{}, providerOp(t, "fal"))
	require.NoError(t, err)
	assert.Equal(t, "fal", f.saved)
	require.Len(t, res.Messages, 1)
	assert.Equal(t, "set_credential", res.Messages[0].RefID)
}

func TestProvider_NoCardWhenNothingChangesOrTheFileIsPinned(t *testing.T) {
	f := &fakeProvider{view: apikey.ProviderView{Selected: "fal"}, keys: map[string]bool{"fal": true}}
	_, err := apikey.NewProvider(f.deps()).Question(context.Background(), domain.Project{}, providerOp(t, "fal"))
	assert.ErrorIs(t, err, chatops.ErrNoChange)

	f.view = apikey.ProviderView{Selected: "official", Pinned: true}
	_, err = apikey.NewProvider(f.deps()).Question(context.Background(), domain.Project{}, providerOp(t, "fal"))
	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Contains(t, ref.Message, "固定")
}

func TestProvider_AKeyAlreadyStoredIsNotAskedAgain(t *testing.T) {
	f := &fakeProvider{view: apikey.ProviderView{Selected: "official"}, keys: map[string]bool{"replicate": true}}

	res, err := apikey.NewProvider(f.deps()).Apply(context.Background(), domain.Project{}, providerOp(t, "replicate"))

	require.NoError(t, err)
	assert.Empty(t, res.Messages)
}
