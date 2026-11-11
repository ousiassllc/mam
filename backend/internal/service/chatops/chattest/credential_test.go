// Package chattest holds the service-level tests of the chat operations of the global settings (#1140): the chat, the confirmation cards and the services behind them,
// with fakes for everything that would cost money or touch the network. They are not in package service because that directory is at its size limit.
package chattest

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/apikey"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// plainKey is the value the tests type into a card: it must be found nowhere but the key store.
const plainKey = "sk-test-0123456789abcdefghijklmnop"

// newKeyFixture is a service without an LLM (the case that matters: the key of the LLM is the first thing to set) with the chat operations wired.
func newKeyFixture(t *testing.T) *chatfix.Fixture {
	t.Helper()
	models := modelsettings.New(filepath.Join(t.TempDir(), "models.json"), false)
	return chatfix.New(t, chatfix.WithoutLLM(), chatfix.Wired(chatwire.Settings{Models: models}))
}

func keyCard(t *testing.T, f *chatfix.Fixture, content string) domain.ThreadMessage {
	t.Helper()
	cards := chatfix.Cards(f.Say(content, "").Messages)
	require.Len(t, cards, 1)
	return cards[0]
}

// dump is every text the database holds, table by table: the plain key must not be in it.
func dump(t *testing.T, f *chatfix.Fixture) string {
	t.Helper()
	rd := f.DB.Reader()
	rows, err := rd.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Close())
	var b strings.Builder
	for _, table := range tables {
		b.WriteString(dumpTable(t, rd, table))
	}
	return b.String()
}

func dumpTable(t *testing.T, rd *sql.DB, table string) string {
	t.Helper()
	rows, err := rd.Query(fmt.Sprintf(`SELECT * FROM "%s"`, table))
	require.NoError(t, err)
	defer rows.Close()
	cols, err := rows.Columns()
	require.NoError(t, err)
	var b strings.Builder
	for rows.Next() {
		vals, ptrs := make([]any, len(cols)), make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		require.NoError(t, rows.Scan(ptrs...))
		for _, v := range vals {
			fmt.Fprintf(&b, "%v|", v)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func requireNoKeyAnywhere(t *testing.T, f *chatfix.Fixture, secret string) {
	t.Helper()
	assert.NotContains(t, dump(t, f), secret, "the database (messages, events, traces, jobs, assets) holds no key")
	assert.NotContains(t, f.Logs.String(), secret, "the log holds no key")
	for _, m := range f.Thread() {
		assert.NotContains(t, m.Content, secret)
	}
}

func requireInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var invalid *service.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, field, invalid.Field)
}

// "OpenAI の API キーを設定したい" in the chat gives a card with a masked field (no LLM is needed), and the card carries no value: the key is typed into the field and posted to
// the card's endpoint, which stores it through the key store of the settings screen. It is then in none of the messages, the database, or the log.
func TestChat_AnAPIKeyIsTypedIntoTheCardAndNeverIntoTheThread(t *testing.T) {
	f := newKeyFixture(t)

	card := keyCard(t, f, "OpenAI の API キーを設定したい")

	body := chatfix.Body(card)
	require.NotNil(t, body.Secret)
	assert.Equal(t, "openai", body.Secret.Provider)
	assert.Equal(t, []string{"やめる"}, body.Options, "no yes button: the key is the confirmation")
	assert.Contains(t, body.Text, "全プロジェクト")
	assert.Contains(t, body.Text, "チャットには書かないでください")
	assert.True(t, card.Open)
	has, _ := f.Keys.Has("openai")
	assert.False(t, has, "nothing is stored before the user submits")

	ws, err := f.Svc.Chat.SubmitSecret(context.Background(), f.ProjectID, card.ID, plainKey)

	require.NoError(t, err)
	got, err := f.Keys.Get("openai")
	require.NoError(t, err)
	assert.Equal(t, plainKey, got, "stored through the key store")
	require.Len(t, ws.Messages, 2)
	assert.Equal(t, card.ID, ws.Messages[0].ReplyToMessageID)
	assert.Equal(t, "「OpenAI」の API キーを入力しました（値はチャットには残しません）", ws.Messages[0].Content, "the whole line the spec gives, with the name of the key")
	assert.Contains(t, ws.Messages[1].Content, "保存しました")
	require.Len(t, ws.Updated, 1)
	assert.False(t, ws.Updated[0].Open, "the card is closed")
	requireNoKeyAnywhere(t, f, plainKey)

	_, err = f.Svc.Chat.SubmitSecret(context.Background(), f.ProjectID, card.ID, plainKey)
	require.ErrorIs(t, err, service.ErrConflict, "a card is answered once")
}

func TestChat_ASecretTheStoreRefusesLeavesTheCardOpenAndIsNotEchoed(t *testing.T) {
	f := newKeyFixture(t)
	card := keyCard(t, f, "OpenAI の API キーを設定したい")

	for _, bad := range []string{"", "   ", "sk with space inside 0123456789"} {
		_, err := f.Svc.Chat.SubmitSecret(context.Background(), f.ProjectID, card.ID, bad)
		requireInvalid(t, err, "secret")
	}

	has, _ := f.Keys.Has("openai")
	assert.False(t, has)
	assert.True(t, chatfix.Cards(f.Thread())[0].Open, "the user can type it again")
}

func TestChat_DecliningTheKeyCardStoresNothing(t *testing.T) {
	f := newKeyFixture(t)
	card := keyCard(t, f, "Claude のキーを登録したい")
	assert.Equal(t, "anthropic", chatfix.Body(card).Secret.Provider)

	ws := f.Answer(card.ID, "やめる")

	assert.Contains(t, chatfix.LastText(ws), "何も変更していません")
	has, _ := f.Keys.Has("anthropic")
	assert.False(t, has)
}

func TestChat_APasteOfAKeyIntoTheComposerIsRecordedHiddenAndNotRead(t *testing.T) {
	f := chatfix.New(t)
	f.Direct()
	f.LLM.Interpreter = chatfix.OpsJSON()

	ws := f.Say("OpenAI のキーは "+plainKey+" です", "")

	require.Len(t, ws.Messages, 2)
	assert.NotContains(t, ws.Messages[0].Content, plainKey)
	assert.Contains(t, ws.Messages[0].Content, "伏せました")
	assert.Contains(t, ws.Messages[1].Content, "カード")
	assert.Empty(t, f.LLM.Asked(llm.AgentCommandInterpreter), "the model never saw the message")
	requireNoKeyAnywhere(t, f, plainKey)
}

func TestChat_ANewKeyCardSupersedesTheOlderOne(t *testing.T) {
	f := newKeyFixture(t)
	older := keyCard(t, f, "OpenAI の API キーを設定したい")
	newer := keyCard(t, f, "OpenAI の API キーを設定したい")

	_, err := f.Svc.Chat.SubmitSecret(context.Background(), f.ProjectID, older.ID, plainKey)
	require.ErrorIs(t, err, service.ErrConflict)
	_, err = f.Svc.Chat.SubmitSecret(context.Background(), f.ProjectID, newer.ID, plainKey)
	require.NoError(t, err)
}

func TestChat_SubmittingToACardWithoutAFieldIsRefused(t *testing.T) {
	f := newKeyFixture(t)
	budget := chatfix.Cards(f.Say("予算は $20 で", "").Messages)
	require.Len(t, budget, 1)

	_, err := f.Svc.Chat.SubmitSecret(context.Background(), f.ProjectID, budget[0].ID, plainKey)

	requireInvalid(t, err, "secret")
	has, _ := f.Keys.Has("openai")
	assert.False(t, has)
}

func TestChat_AMessageThatNamesNoProviderAsksWhich(t *testing.T) {
	f := newKeyFixture(t)

	ws := f.Say("API キーを設定したい", "")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "どのサービスの API キーですか")
}

// The video provider (#1136): confirmed first; one that has no key yet is followed by the card that asks for it; the same models.json as the screen.
func TestChat_ChoosingTheVideoProviderAsksFirstAndThenAsksForItsKey(t *testing.T) {
	f := newKeyFixture(t)

	card := keyCard(t, f, "Kling は OpenRouter 経由で使いたい")

	assert.Equal(t, "set_video_provider", card.RefID)
	text := chatfix.Body(card).Text
	assert.Contains(t, text, "OpenRouter")
	assert.Contains(t, text, "全プロジェクト")
	assert.Contains(t, text, "API キーがまだありません")

	ws := f.Answer(card.ID, "はい")

	assert.Contains(t, ws.Messages[1].Content, "呼び出し先を「OpenRouter」にしました")
	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 1, "the card that asks for the key follows")
	assert.Equal(t, "openrouter", chatfix.Body(cards[0]).Secret.Provider)
	assert.Equal(t, "set_credential", cards[0].RefID)
}

func TestChat_TheKeyOperationsAreTheSameOnesThePackageDocuments(t *testing.T) {
	ids := apikey.IDs()
	require.Len(t, ids, len(service.CredentialProviders))
	for i, p := range service.CredentialProviders {
		assert.Equal(t, p.ID, ids[i], "the order of the settings screen")
		label, ok := apikey.Label(p.ID)
		assert.True(t, ok)
		assert.Equal(t, p.Label, label)
	}
}

// With an LLM, the model's answer and the rule meet: a message the model did not turn into an operation still gets the key card, in AUTO MODE and in DIRECTOR MODE alike
// (the API key is a setting of the whole app, not a direction of a Shot).
func TestChat_TheKeyCardComesInEveryModeEvenWhenTheModelReturnsNothing(t *testing.T) {
	for name, prepare := range map[string]func(f *chatfix.Fixture){"DIRECTOR MODE": (*chatfix.Fixture).Direct, "AUTO MODE": (*chatfix.Fixture).Auto} {
		t.Run(name, func(t *testing.T) {
			f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
			prepare(f)
			f.LLM.Interpreter = chatfix.OpsJSON()

			cards := chatfix.Cards(f.Say("Gemini の API キーを入れ替えたい", "").Messages)

			require.Len(t, cards, 1)
			assert.Equal(t, "google", chatfix.Body(cards[0]).Secret.Provider)
		})
	}
}

// The model's word alone does not ask for a key: an operation it returns for a message about something else is dropped.
func TestChat_ASetCredentialTheMessageDoesNotAskForIsDropped(t *testing.T) {
	f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
	f.Direct()

	ws := f.SayOps("カメラを固定して", `{"type":"set_credential","args":{"provider":"openai"}}`)

	assert.Empty(t, chatfix.Cards(ws.Messages))
}

// What the chat refuses on purpose (the program paths of ffmpeg and FaceFusion, the tokens of MCP) is answered with where to do it, and nothing is read as an operation.
func TestChat_ProgramPathsAndMCPTokensAreRefusedWithAReason(t *testing.T) {
	f := newKeyFixture(t)

	for message, mention := range map[string]string{
		"ffmpeg のパスを設定して": "ffmpeg と FaceFusion の実行パス",
		"MCP のトークンを発行して":  "MCP のトークン",
	} {
		ws := f.Say(message, "")

		assert.Empty(t, chatfix.Cards(ws.Messages), message)
		assert.Contains(t, chatfix.LastText(ws), mention, message)
		assert.Contains(t, chatfix.LastText(ws), "設定してください", message)
	}
}

// An operation whose service the app did not wire says so instead of failing.
func TestChat_AnOperationWithoutItsServiceSaysItIsNotAvailable(t *testing.T) {
	f := chatfix.New(t, chatfix.WithoutLLM()) // no chatwire.Connect

	ws := f.Say("OpenAI の API キーを設定したい", "")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "この環境では、その設定をチャットから変更できません")
}
