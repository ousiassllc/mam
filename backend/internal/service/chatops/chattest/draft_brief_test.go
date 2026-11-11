package chattest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
)

func briefOf(t *testing.T, f *chatfix.Fixture) domain.Brief {
	t.Helper()
	view, err := f.Svc.Projects.Brief(context.Background(), f.ProjectID)
	require.NoError(t, err)
	return view.Brief
}

// While the Brief is drafted, what sounds like a setting of the app ("動画は縦型にして", "参照画像を使って", "モデルさんを入れて") is an answer to the Brief: it is read by the Brief extractor,
// never by the Command Interpreter (#1140 reached the director side with every message that named a medium and asked for a change).
func TestChat_BriefWordsInADraftProjectAreReadAsTheBriefNotAsSettings(t *testing.T) {
	for _, message := range []string{
		"動画は縦型の15秒にして",
		"動画の長さを15秒にして",
		"雰囲気は明るい画像にして",
		"参照画像を使って",
		"出演者はプロのモデルを入れて",
		"モデルさんを入れて",
	} {
		t.Run(message, func(t *testing.T) {
			f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
			f.LLM.Interpreter = chatfix.OpsJSON(`{"type":"create_note","content":"x"}`)

			ws := f.Say(message, "")

			assert.Empty(t, f.LLM.Asked(llm.AgentCommandInterpreter), "the Command Interpreter never saw it")
			assert.Empty(t, chatfix.Cards(ws.Messages))
			for _, m := range ws.Messages {
				assert.NotContains(t, m.Content, "読み取れる内容が見つかりませんでした")
			}
		})
	}
}

func TestChat_AVerticalFifteenSecondDirectionInADraftProjectFillsTheBrief(t *testing.T) {
	for message, want := range map[string]func(b domain.Brief){
		"動画は縦型の15秒にして": func(b domain.Brief) {
			assert.Equal(t, "15秒", b.Duration)
			assert.Equal(t, "9:16", b.AspectRatio)
		},
		"動画の長さを15秒にして": func(b domain.Brief) { assert.Equal(t, "15秒", b.Duration) },
	} {
		f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
		require.Empty(t, briefOf(t, f).Duration)

		f.Say(message, "")

		want(briefOf(t, f))
	}
}

// The settings a new app needs first still work in a DRAFT Project, and so does what the chat refuses on purpose.
func TestChat_TheKeyAndTheRefusalsStillReachTheDirectorSideInADraftProject(t *testing.T) {
	f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
	require.Equal(t, domain.StateDraft, mustProject(t, f).State)

	cards := chatfix.Cards(f.Say("OpenAI の API キーを設定したい", "").Messages)
	require.Len(t, cards, 1)
	assert.Equal(t, "openai", chatfix.Body(cards[0]).Secret.Provider)

	ws := f.Say("ffmpeg のパスを設定して", "")
	assert.Contains(t, chatfix.LastText(ws), "ffmpeg と FaceFusion の実行パス")
}

// Past the Brief the settings that name their target are read in any mode, the Brief's own words are not.
func TestChat_PastTheBriefTheSettingsStillCardAndTheBriefsWordsStillDoNot(t *testing.T) {
	f, _ := assignFixture(t)
	f.Auto()
	f.LLM.Interpreter = chatfix.OpsJSON(`{"type":"set_model_assignment","args":{"target":"new_reference","model":"flux"}}`)

	cards := chatfix.Cards(f.Say("生成の画像モデルの 新規参照 を flux にして", "").Messages)
	require.Len(t, cards, 1)
	assert.Equal(t, "set_model_assignment", cards[0].RefID)

	before := len(f.LLM.Asked(llm.AgentCommandInterpreter))
	f.Say("動画は縦型にして", "")
	assert.Len(t, f.LLM.Asked(llm.AgentCommandInterpreter), before, "a direction of the video is not read as a setting")
}

func mustProject(t *testing.T, f *chatfix.Fixture) domain.Project {
	t.Helper()
	p, err := f.Svc.Projects.Get(context.Background(), f.ProjectID)
	require.NoError(t, err)
	return p
}
