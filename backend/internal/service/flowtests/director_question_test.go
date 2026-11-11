package flowtests_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/apikey"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// When the model answers with a question, what the message plainly asks for (WithDetected: "OpenAI の API キーを設定したい" is the key card) is still carried out, in DIRECTOR MODE
// and in AUTO MODE alike: the question is told with the card, not instead of it.
func TestChat_AQuestionOfTheModelDoesNotDropTheCardTheMessagePlainlyAsksFor(t *testing.T) {
	const ask = "どの Shot への指示ですか？"
	for _, direct := range []bool{true, false} {
		t.Run(map[bool]string{true: "director", false: "auto"}[direct], func(t *testing.T) {
			f := chatfix.New(t, chatfix.Wired(chatwire.Settings{Models: modelsettings.New(filepath.Join(t.TempDir(), "models.json"), false)}))
			f.LLM.Interpreter = func(string) string { return `{"question":"` + ask + `"}` }
			if direct {
				f.Direct()
			} else {
				f.Auto()
			}

			ws := f.Say("OpenAI の API キーを設定したい", "")

			cards := chatfix.Cards(ws.Messages)
			require.Len(t, cards, 1, "the key card is posted")
			assert.Equal(t, string(apikey.OpSetCredential), cards[0].RefID)
			assert.True(t, slices.ContainsFunc(ws.Messages, func(m domain.ThreadMessage) bool {
				return m.Kind == domain.KindText && m.Role == domain.RoleAssistant && m.Content == ask
			}), "the question of the model is told too")
		})
	}
}

// The yes of a confirmation card whose button carries no action (the stored "null") is no yes: the reply is a new message, which the card does not carry out.
func TestChat_AYesToACardWhoseButtonCarriesNoActionIsNotCarriedOut(t *testing.T) {
	f := chatfix.New(t)
	body := domain.EncodeCardBody(domain.CardBody{Text: "進めますか？", Options: []string{chatops.Yes, chatops.No}, OptionActions: []json.RawMessage{json.RawMessage("null"), nil}})
	f.SQL(`INSERT INTO chat_message (id, project_id, role, kind, content, ref_type, ref_id) VALUES ('card1', ?, 'assistant', 'question', ?, 'operation', 'set_budget')`, f.ProjectID, body)

	ws := f.Say("はい", "card1")

	assert.NotContains(t, chatfix.LastText(ws), "実行しました")
	assert.NotContains(t, chatfix.LastText(ws), "やめました")
	assert.True(t, slices.ContainsFunc(ws.Updated, func(m domain.ThreadMessage) bool { return m.ID == "card1" && !m.Open }), "a new message replies to the card, which closes it, as for any other reply that is no option")
}

// The yes button is found by its words, wherever it stands in the card: a yes that is not the first button runs its own action, and when that action is the stored "null" the action of
// another button is not run in its place.
func TestChat_TheYesButtonThatIsNotFirstRunsItsOwnActionOnly(t *testing.T) {
	budget := json.RawMessage(`{"type":"set_budget","project_budget_usd":99}`)
	for name, tc := range map[string]struct {
		actions []json.RawMessage
		want    sql.NullFloat64
	}{
		"its own action":       {actions: []json.RawMessage{json.RawMessage("null"), budget}, want: sql.NullFloat64{Float64: 99, Valid: true}},
		"no action of its own": {actions: []json.RawMessage{budget, json.RawMessage("null")}},
	} {
		t.Run(name, func(t *testing.T) {
			f := chatfix.New(t)
			body := domain.EncodeCardBody(domain.CardBody{Text: "進めますか？", Options: []string{chatops.No, chatops.Yes}, OptionActions: tc.actions})
			f.SQL(`INSERT INTO chat_message (id, project_id, role, kind, content, ref_type, ref_id) VALUES ('card1', ?, 'assistant', 'question', ?, 'operation', 'set_budget')`, f.ProjectID, body)

			f.Say(chatops.Yes, "card1")

			var ceiling sql.NullFloat64
			require.NoError(t, f.DB.Reader().QueryRow(`SELECT project_budget_ceiling FROM project WHERE id = ?`, f.ProjectID).Scan(&ceiling))
			assert.Equal(t, tc.want, ceiling)
		})
	}
}
