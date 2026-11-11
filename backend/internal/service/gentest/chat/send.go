package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/secretguard"
)

// Send takes a message of the user and answers it: the LLM acts step by step (it may look at the models and price batches) until it says something, puts
// options on cards, or an estimate becomes a confirmation card. Nothing is generated here. The reply is the messages to add to the thread, the user's first.
//
// An error is returned for a message that cannot be taken (ErrBadMessage, ErrNoSession) or a cancelled request, and then the conversation keeps nothing of the
// message; a failure of the LLM is a message of the reply (the message is kept: the user said it).
func (s *Service) Send(ctx context.Context, sessionID, text string) (Reply, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxMessageRunes {
		return Reply{}, ErrBadMessage
	}
	text, _ = secretguard.Redact(text) // an API key is never sent to the LLM nor kept in the thread (spec/components/chat-settings-operations.md §1.1, #1140)
	se, err := s.open(sessionID, true)
	if err != nil {
		return Reply{}, err
	}
	se.send.Lock() // the LLM calls below do not hold se.mu: the buttons of the cards work while the message is answered
	defer se.send.Unlock()

	rep := Reply{Messages: []Message{{ID: newID(), Kind: KindUser, Text: text}}}
	x := &exchange{se: se}
	x.add(gentestchat.RoleUser, text)
	if err := s.answer(ctx, x, &rep); err != nil {
		return rep, err
	}
	x.commit()
	return rep, nil
}

// answer runs the steps of the LLM for the message of x until the turn is over.
func (s *Service) answer(ctx context.Context, x *exchange, rep *Reply) error {
	items, cost := s.d.Gentest.Limits()
	limits := gentestchat.Limits{MaxItems: items, MaxCostUSD: cost}
	retried := false
	for range MaxSteps {
		st, err := s.d.Agent.Next(ctx, gentestchat.Input{Turns: x.shown(), Limits: limits})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if gentestchat.IsBadStep(err) && !retried { // one more try, told what was wrong
				retried = true
				x.add(gentestchat.RoleTool, "Your last answer was not one of the allowed JSON actions ("+gentestchat.BadStepReason(err)+"). Answer with exactly one JSON object.")
				continue
			}
			s.d.Gentest.Logger.Warn("gentest chat: the LLM failed", "err", err)
			rep.Messages = append(rep.Messages, failure(err))
			return nil
		}
		x.add(gentestchat.RoleAssistant, stepJSON(st))
		done, err := s.act(ctx, x, st, rep)
		if err != nil || done {
			return err
		}
	}
	rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindAssistant,
		Text: "まだ決めきれていません。作りたいものや使いたいモデルを、もう少し具体的に教えてください。"})
	return nil
}

// act carries out one step. done says the turn is over (the user has something to read or press).
func (s *Service) act(ctx context.Context, x *exchange, st gentestchat.Step, rep *Reply) (done bool, err error) {
	switch st.Action {
	case gentestchat.ActionSay:
		rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindAssistant, Text: st.Text})
		return true, nil
	case gentestchat.ActionChoose:
		rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindAssistant, Text: st.Text, Options: st.Options})
		return true, nil
	case gentestchat.ActionListModels:
		text, status, err := s.listModels(ctx, st.ModelQuery)
		if err != nil {
			return false, err
		}
		x.add(gentestchat.RoleTool, text)
		rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindStatus, Text: status})
		return false, nil
	case gentestchat.ActionEstimate:
		return s.estimate(ctx, x, st, rep)
	}
	return true, nil // clean() refuses every other action
}

// failure is the message of a LLM that could not answer, in the words the other LLM-backed parts of the generation test use.
func failure(err error) Message {
	m := Message{ID: newID(), Kind: KindStatus, Failed: true}
	switch {
	case errors.Is(err, llm.ErrNoAPIKey):
		m.Text, m.LLMKey = failmsg.NoKeyMessage(err), true
	case errors.Is(err, llm.ErrCircuitOpen):
		m.Text = "LLM が一時的に使えません。しばらく待ってからもう一度試してください"
	case errors.Is(err, llm.ErrTimeout):
		m.Text = "LLM の応答が時間内に返りませんでした"
	case errors.Is(err, llm.ErrInvalidOutput):
		m.Text = "LLM の答えが使えない形でした。もう一度送ってみてください"
	default:
		m.Text = "LLM の呼び出しに失敗しました。「LLM の割り当て」の設定を確認してください"
	}
	return m
}

// recent is the newest turns the LLM is shown.
func recent(turns []gentestchat.Turn) []gentestchat.Turn {
	if len(turns) > MaxTurns {
		return turns[len(turns)-MaxTurns:]
	}
	return turns
}

func stepJSON(st gentestchat.Step) string {
	raw, err := json.Marshal(st)
	if err != nil {
		return st.Action
	}
	return string(raw)
}
