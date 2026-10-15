package service

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func (s *DirectorService) interpret(ctx context.Context, message string, shots []repository.ShotRef, target *repository.ShotRef) (commandinterpreter.Command, error) {
	in := commandinterpreter.Input{Message: message}
	for _, sh := range shots {
		in.Shots = append(in.Shots, optionOf(sh))
	}
	if target != nil {
		opt := optionOf(*target)
		in.Target = &opt
		if spec, ok := target.Shot.CanonicalSpec(); ok {
			in.Spec = &spec
		}
		locks, err := s.locks.ShotLocks(ctx, target.Shot.ID)
		if err != nil {
			return commandinterpreter.Command{}, err
		}
		in.Locks = locks
	}
	cmd, err := s.interpreter.Interpret(ctx, in)
	if err != nil {
		return cmd, err
	}
	if !domain.AsksToRemember(message) {
		// A note is made only when the message asks for it (spec/spec.md §9.4): the model's
		// word alone is not enough in any mode.
		cmd.Operations = slices.DeleteFunc(cmd.Operations, func(op commandinterpreter.Operation) bool { return op.Type == commandinterpreter.OpCreateNote })
	}
	return cmd, nil
}

// interpretError gives a failure of the Command Interpreter's LLM call the status the API
// documents: no API key is 503 (ErrUnavailable), a 429 is ErrRateLimited (through the AIError), and a
// timeout, an open circuit breaker, a failing API or an answer that is not the expected JSON is 502. Errors of other origin
// (the repository) are returned as they are.
func interpretError(err error) error {
	switch {
	case errors.Is(err, llm.ErrNoAPIKey):
		return ErrUnavailable
	case isInterpreterAIError(err):
		return aiError(err)
	}
	return err
}

// isInterpreterAIError reports whether err is a failure of the Command Interpreter's LLM call (as
// opposed to one of the repository reads around it).
func isInterpreterAIError(err error) bool {
	var api *llm.APIError
	return errors.Is(err, llm.ErrNoAPIKey) || errors.Is(err, llm.ErrInvalidOutput) || errors.Is(err, llm.ErrTimeout) ||
		errors.Is(err, llm.ErrCircuitOpen) || errors.As(err, &api)
}

func optionOf(sh repository.ShotRef) commandinterpreter.ShotOption {
	return commandinterpreter.ShotOption{Scene: sh.Scene.OrderIndex + 1, Shot: sh.Shot.OrderIndex + 1, Objective: sh.Shot.Objective}
}

// outcome is what the operations of one direction did, as lines for the thread.
type outcome struct {
	lines   []string
	started bool
}

func (o outcome) text() string {
	if len(o.lines) == 0 {
		return "指示として実行できる内容が見つかりませんでした。対象の Shot と変えたい点（例:「Scene 1 の Shot 2 のカメラを固定して」）を教えてください。"
	}
	return strings.Join(o.lines, "\n")
}

const askWhichShot = "どの Shot への指示ですか？ Take カードに返信するか、「Scene 1 の Shot 2」のように指定してください。"

// unsupportedFixed is the reply to Example 4 (spec/spec.md): a timeline edit or an extra Shot asked
// for in chat. The model's own reason is not used for those.
const unsupportedFixed = "その操作にはまだ対応していません"

// unsupportedReply is what the thread shows for an unsupported direction. A request to change the
// Edit Timeline or to add a Shot always gets unsupportedFixed, even when the interpreter wrote
// another sentence. Other unsupported directions keep that sentence, or the fixed one when it is empty.
func unsupportedReply(message, reason string) string {
	if isTimelineOrExtraShot(message) || strings.TrimSpace(reason) == "" {
		return unsupportedFixed
	}
	return strings.TrimSpace(reason)
}

// isTimelineOrExtraShot reports whether the director asked to change a clip's length (Example 4) or
// to add a Shot. Chat does not do either.
func isTimelineOrExtraShot(message string) bool {
	m := strings.ToLower(message)
	if strings.Contains(m, "タイムライン") || strings.Contains(m, "timeline") || strings.Contains(m, "尺") {
		return true
	}
	if strings.Contains(m, "秒") && (strings.Contains(m, "長") || strings.Contains(m, "短")) {
		return true
	}
	aboutShot := strings.Contains(m, "ショット") || strings.Contains(m, "shot") || strings.Contains(m, "カット")
	asksMore := strings.Contains(m, "追加") || strings.Contains(m, "足して") || strings.Contains(m, "増や")
	return aboutShot && asksMore
}
