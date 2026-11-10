package chattest

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/secretguard"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
)

// providerKeys are keys of the shapes of the providers that have no prefix of their own, built at run time (no real key is in the repository).
func providerKeys() map[string]string {
	hex := strings.Repeat("0123456789abcdef", 4)
	mixed := func(n int) string { return strings.Repeat("AbCdEf0123456789", n)[:n] }
	return map[string]string{
		"kling (32 letters and digits)": mixed(32),
		"runway (key_ + hex)":           "key" + "_" + hex[:48],
		"luma (luma- + uuid)":           "luma" + "-" + "12345678-1234-1234-1234-123456789abc",
		"minimax (JWT)":                 "ey" + "J" + mixed(30) + "." + "ey" + "J" + mixed(40) + "." + mixed(43),
		"seedance / flux (uuid)":        "12345678-1234-1234-1234-123456789abc",
		"sync labs":                     "sk" + "-" + hex[:40],
	}
}

// recordingBriefing is the Brief extractor of the fixture: it keeps every text it was given.
type recordingBriefing struct{ seen []string }

func (r *recordingBriefing) Extract(text string) domain.Brief {
	r.seen = append(r.seen, text)
	return briefing.RuleExtractor{}.Extract(text)
}

func (r *recordingBriefing) read(secret string) bool {
	for _, text := range r.seen {
		if strings.Contains(text, secret) {
			return true
		}
	}
	return false
}

// seenByAModel reports whether the key reached the Brief extractor or any Agent of the LLM.
func seenByAModel(f *chatfix.Fixture, extractor *recordingBriefing, secret string) bool {
	for _, agent := range llm.Agents() {
		for _, prompt := range f.LLM.Asked(agent) {
			if strings.Contains(prompt, secret) {
				return true
			}
		}
	}
	return extractor.read(secret)
}

func keyFixture(t *testing.T) (*chatfix.Fixture, *recordingBriefing) {
	t.Helper()
	extractor := &recordingBriefing{}
	f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}), chatfix.WithDeps(func(d *service.Deps) { d.Briefing = extractor }))
	f.Direct()
	f.LLM.Interpreter = chatfix.OpsJSON()
	return f, extractor
}

// A key whose provider has no shape of its own (Kling, Runway, Luma, MiniMax, Seedance, Flux, Sync Labs) is found by being one long token: pasted bare, in a sentence, or typed into
// the composer while no card is open, it is recorded hidden and read by no model.
func TestChat_AKeyOfAnyProviderShapeIsRecordedHiddenAndReadByNoModel(t *testing.T) {
	for name, key := range providerKeys() {
		t.Run(name, func(t *testing.T) {
			for _, message := range []string{key, "これを使って " + key + " お願いします"} {
				f, extractor := keyFixture(t)

				ws := f.Say(message, "")

				require.Len(t, ws.Messages, 2)
				assert.Contains(t, ws.Messages[0].Content, secretguard.Hidden)
				assert.Contains(t, ws.Messages[1].Content, "カード")
				assert.False(t, seenByAModel(f, extractor, key[len(key)/3:]), "no model saw it")
				requireNoKeyAnywhere(t, f, key)
				requireNoKeyAnywhere(t, f, key[len(key)/3:])
			}
		})
	}
}

// While the card that asks for a key is open, one word of the shape of a key nobody knows is a key; a reply to the card that is no decline is recorded hidden whole; neither
// answers the card (it stays open, and the key typed into its field is stored afterwards).
func TestChat_WhileTheKeyCardIsOpenOneWordOrAReplyIsHiddenAndTheCardStaysOpen(t *testing.T) {
	f := newKeyFixture(t)
	card := keyCard(t, f, "OpenAI の API キーを設定したい")
	short := "Zm9vYmFy+/=1"

	ws := f.Say(short, "")
	require.Len(t, ws.Messages, 2)
	assert.Equal(t, secretguard.Hidden, ws.Messages[0].Content)
	assert.Empty(t, ws.Messages[0].ReplyToMessageID)

	ws = f.Answer(card.ID, "はい これです abc")
	assert.Equal(t, secretguard.Hidden, ws.Messages[0].Content)
	assert.Empty(t, ws.Messages[0].ReplyToMessageID, "the card is not answered by it")

	ws = f.Answer(card.ID, "sk-"+strings.Repeat("a1", 15))
	assert.Empty(t, ws.Messages[0].ReplyToMessageID)

	cards := chatfix.Cards(f.Thread())
	require.Len(t, cards, 1)
	assert.True(t, cards[0].Open, "the card still waits for its key")
	requireNoKeyAnywhere(t, f, short)

	_, err := f.Svc.Chat.SubmitSecret(context.Background(), f.ProjectID, card.ID, plainKey)
	require.NoError(t, err, "the field still works")
	got, err := f.Keys.Get("openai")
	require.NoError(t, err)
	assert.Equal(t, plainKey, got)
	requireNoKeyAnywhere(t, f, plainKey)
}

func TestChat_AKeyCardClosedOrDeclinedTakesOrdinaryWordsAgain(t *testing.T) {
	f := newKeyFixture(t)
	card := keyCard(t, f, "OpenAI の API キーを設定したい")

	ws := f.Answer(card.ID, "やめる")
	assert.Contains(t, chatfix.LastText(ws), "何も変更していません", "a decline is the one reply the card takes")

	ws = f.Say("Zm9vYmFy+/=1", "")
	assert.NotContains(t, ws.Messages[0].Content, secretguard.Hidden, "no card waits: a single word is a message")
	ws = f.Say("OpenAI の API キーを設定したい", "")
	assert.NotEmpty(t, chatfix.Cards(ws.Messages))
	ws = f.Say("予算は $20 で", "")
	assert.NotContains(t, ws.Messages[0].Content, secretguard.Hidden, "words with spaces and Japanese are not a key")
}

// The other entrances of a user's words keep the key out of the thread and away from the models as PostMessage does.
func TestProjectCreate_AKeyInTheFirstMessageIsHiddenAndNotExtracted(t *testing.T) {
	for name, key := range providerKeys() {
		t.Run(name, func(t *testing.T) {
			f, extractor := keyFixture(t)

			p, err := f.Svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "美容液のCMを作りたい。キーは " + key})

			require.NoError(t, err)
			ws, err := f.Svc.Chat.Workspace(context.Background(), p.ID)
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(ws.Messages), 2)
			assert.Contains(t, ws.Messages[0].Content, secretguard.Hidden)
			assert.Contains(t, ws.Messages[1].Content, "カード")
			assert.False(t, extractor.read(key[len(key)/3:]), "the extractor never saw it")
			assert.False(t, seenByAModel(f, extractor, key[len(key)/3:]))
			requireNoKeyAnywhere(t, f, key)
			requireNoKeyAnywhere(t, f, key[len(key)/3:])
		})
	}
}

func TestDirectorCommand_AKeyInADirectionIsHiddenAndNotInterpreted(t *testing.T) {
	for name, key := range providerKeys() {
		t.Run(name, func(t *testing.T) {
			f, extractor := keyFixture(t)
			ctx := context.Background()
			scene, err := f.Svc.Plans.CreateScene(ctx, f.ProjectID, service.SceneForm{Title: "Street"})
			require.NoError(t, err)
			shot, err := f.Svc.Plans.CreateShot(ctx, scene.Scene.ID, service.ShotForm{Objective: "走り出す", DurationSeconds: "4"})
			require.NoError(t, err)

			_, err = f.Svc.Director.DirectorCommand(ctx, shot.Shots[0].ID, "もう少し寄って "+key)

			require.NoError(t, err)
			assert.Empty(t, f.LLM.Asked(llm.AgentCommandInterpreter), "the Interpreter never saw it")
			assert.False(t, seenByAModel(f, extractor, key[len(key)/3:]))
			thread := f.Thread()
			require.GreaterOrEqual(t, len(thread), 2)
			assert.Contains(t, thread[len(thread)-2].Content, secretguard.Hidden)
			assert.Contains(t, thread[len(thread)-1].Content, "カード")
			requireNoKeyAnywhere(t, f, key)
			requireNoKeyAnywhere(t, f, key[len(key)/3:])
		})
	}
}

func TestRespondApproval_AKeyInTheCommentIsHiddenAndTheDecisionStands(t *testing.T) {
	for name, key := range providerKeys() {
		t.Run(name, func(t *testing.T) {
			f, extractor := keyFixture(t)
			ctx := context.Background()
			posted, err := f.Svc.Chat.RequestApproval(ctx, f.ProjectID, domain.CheckpointScript, "脚本 v1", "sv1")
			require.NoError(t, err)
			require.True(t, posted)
			var approval domain.ThreadMessage
			for _, m := range f.Thread() {
				if m.Kind == domain.KindApproval {
					approval = m
				}
			}
			require.NotEmpty(t, approval.ID)

			_, card, err := f.Svc.Chat.RespondApproval(ctx, f.ProjectID, approval.ID, service.DecisionRequestChanges, "直して "+key)

			require.NoError(t, err)
			assert.Equal(t, domain.ApprovalChangesRequested, card.ApprovalStatus, "the decision is carried out")
			assert.False(t, seenByAModel(f, extractor, key[len(key)/3:]))
			var hidden bool
			for _, m := range f.Thread() {
				hidden = hidden || strings.Contains(m.Content, secretguard.Hidden)
			}
			assert.True(t, hidden, "the comment is recorded with the key hidden")
			requireNoKeyAnywhere(t, f, key)
			requireNoKeyAnywhere(t, f, key[len(key)/3:])
		})
	}
}
