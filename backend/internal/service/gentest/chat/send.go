package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
)

// Send takes a message of the user and answers it: the LLM acts step by step (it may look at the models and price batches) until it says something, puts
// options on cards, or an estimate becomes a confirmation card. Nothing is generated here. The reply is the messages to add to the thread, the user's first.
//
// An error is returned for a message that cannot be taken (ErrBadMessage, ErrNoSession) or a cancelled request; a failure of the LLM is a message of the reply.
func (s *Service) Send(ctx context.Context, sessionID, text string) (Reply, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxMessageRunes {
		return Reply{}, ErrBadMessage
	}
	se, err := s.open(sessionID, true)
	if err != nil {
		return Reply{}, err
	}
	se.mu.Lock()
	defer se.mu.Unlock()

	rep := Reply{Messages: []Message{{ID: newID(), Kind: KindUser, Text: text}}}
	se.turns = append(se.turns, gentestchat.Turn{Role: gentestchat.RoleUser, Text: text})
	items, cost := s.d.Gentest.Limits()
	limits := gentestchat.Limits{MaxItems: items, MaxCostUSD: cost}
	retried := false
	for range MaxSteps {
		st, err := s.d.Agent.Next(ctx, gentestchat.Input{Turns: recent(se.turns), Limits: limits})
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		if err != nil {
			if gentestchat.IsBadStep(err) && !retried { // one more try, told what was wrong
				retried = true
				se.turns = append(se.turns, gentestchat.Turn{Role: gentestchat.RoleTool, Text: "Your last answer was not one of the allowed JSON actions. Answer with exactly one JSON object."})
				continue
			}
			s.d.Gentest.Logger.Warn("gentest chat: the LLM failed", "err", err)
			rep.Messages = append(rep.Messages, failure(err))
			return rep, nil
		}
		se.turns = append(se.turns, gentestchat.Turn{Role: gentestchat.RoleAssistant, Text: stepJSON(st)})
		done, err := s.act(ctx, se, st, &rep)
		if err != nil {
			return rep, err
		}
		if done {
			return rep, nil
		}
	}
	rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindAssistant,
		Text: "まだ決めきれていません。作りたいものや使いたいモデルを、もう少し具体的に教えてください。"})
	return rep, nil
}

// act carries out one step. done says the turn is over (the user has something to read or press).
func (s *Service) act(ctx context.Context, se *session, st gentestchat.Step, rep *Reply) (done bool, err error) {
	switch st.Action {
	case gentestchat.ActionSay:
		rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindAssistant, Text: st.Text})
		return true, nil
	case gentestchat.ActionChoose:
		rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindAssistant, Text: st.Text, Options: st.Options})
		return true, nil
	case gentestchat.ActionListModels:
		text, n, err := s.listModels(ctx, st.ModelQuery)
		if err != nil {
			return false, err
		}
		se.turns = append(se.turns, gentestchat.Turn{Role: gentestchat.RoleTool, Text: text})
		rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindStatus, Text: modelsStatus(st.ModelQuery, n)})
		return false, nil
	case gentestchat.ActionEstimate:
		return s.estimate(ctx, se, st, rep)
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

func itoa(n int) string { return strconv.Itoa(n) }
