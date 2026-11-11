package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/approvalack"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// The operations of the registry (commandinterpreter/ops.go) that change a setting of the Project are carried out here. An operation that asks for confirmation
// (Spec.Confirm) is not run when it is read: the chat posts a confirmation card that carries it as the action of its "yes" button (domain.CardBody.OptionActions),
// and the answer runs it (ChatService.answerOperation). A card that offers choices (the Escalation of a plan that passes the budget) carries one action per button
// the same way. A new operation is one entry of opHandlers, and one Spec in the interpreter's registry.
type opHandler struct {
	// question is the sentence of the confirmation card of an operation that asks for confirmation. errNoChange says the operation would change nothing.
	question func(s *DirectorService, ctx context.Context, project domain.Project, op commandinterpreter.Operation) (string, error)
	// apply carries the operation out, as the user confirmed it or picked it, and says what it did.
	apply func(s *DirectorService, ctx context.Context, project domain.Project, op commandinterpreter.Operation) (string, error)
	// chat is the handler of an operation on the global settings (service/chatops, #1140); it replaces question and apply, and its card may be richer than a sentence.
	chat chatops.Handler
}

var opHandlers = map[commandinterpreter.OpType]opHandler{
	commandinterpreter.OpSetBudget:        {question: (*DirectorService).askBudget, apply: (*DirectorService).applyBudget},
	commandinterpreter.OpSetQualityPolicy: {apply: (*DirectorService).applyQualityPolicy},
	commandinterpreter.OpSetMode:          {apply: (*DirectorService).applyModeAction},
}

// errNoChange is the answer of a question that finds the operation already done.
var errNoChange = chatops.ErrNoChange

// handlerOf is the handler of an operation: one of the Project's settings (opHandlers) or one of the global settings the app wired (service/chatwire).
func (s *DirectorService) handlerOf(t commandinterpreter.OpType) (opHandler, bool) {
	if h, ok := opHandlers[t]; ok {
		return h, true
	}
	if h, ok := s.chatOps.Handler(t); ok {
		return opHandler{chat: h}, true
	}
	return opHandler{}, false
}

// notWired is what the chat says of an operation on the global settings that this app has no service for (a build without the model manager).
const notWired = "この環境では、その設定をチャットから変更できません。設定画面から行ってください。"

// actionResult is what carrying out an action did: the sentence for the thread and the messages that follow it. failed is true when the operation could not run for a reason
// the user can act on (text says it): nothing was changed, and the sentence must not be told as "done".
type actionResult struct {
	text     string
	messages []domain.ChatMessage
	failed   bool
}

// The buttons of a confirmation card, and the words that answer one in the composer.
const (
	confirmYes = chatops.Yes
	confirmNo  = chatops.No
)

// confirmCard builds the confirmation card of op. When the operation cannot be asked (it is invalid, or already done) the sentence to tell instead is returned and the
// card is empty.
func (s *DirectorService) confirmCard(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (domain.ChatMessage, string, error) {
	h, ok := s.handlerOf(op.Type)
	if !ok {
		if spec, registered := commandinterpreter.SpecOf(op.Type); registered && spec.Global {
			return domain.ChatMessage{}, notWired, nil
		}
		return domain.ChatMessage{}, "", fmt.Errorf("the operation %q asks for confirmation but has no handler (opHandlers)", op.Type)
	}
	card, err := s.ask(ctx, project, h, op)
	var (
		ve  *ValidationError
		ref *chatops.Refusal
	)
	switch {
	case errors.Is(err, errNoChange):
		return domain.ChatMessage{}, "すでにその内容です。変更しませんでした。", nil
	case errors.As(err, &ve):
		return domain.ChatMessage{}, "設定できませんでした: " + ve.Message, nil
	case errors.As(err, &ref):
		return domain.ChatMessage{}, "設定できませんでした: " + ref.Message, nil
	case err != nil:
		return domain.ChatMessage{}, "", err
	}
	msg, err := chatops.Message(op, card)
	return msg, "", err
}

func (s *DirectorService) ask(ctx context.Context, project domain.Project, h opHandler, op commandinterpreter.Operation) (chatops.Card, error) {
	switch {
	case h.chat != nil:
		return h.chat.Question(ctx, project, op)
	case h.question != nil:
		text, err := h.question(s, ctx, project, op)
		return chatops.Card{Text: text}, err
	}
	return chatops.Card{}, fmt.Errorf("the operation %q asks for confirmation but has no question (opHandlers)", op.Type)
}

// applyAction carries out the operation a card button carries (the JSON stored in CardBody.OptionActions), only when it is a valid operation of the registry. A reason the user can act on (an amount the budget refuses) is the sentence returned; any other failure is the error.
func (s *DirectorService) applyAction(ctx context.Context, project domain.Project, raw json.RawMessage) (actionResult, error) {
	var op commandinterpreter.Operation
	if err := json.Unmarshal(raw, &op); err != nil {
		return actionResult{}, fmt.Errorf("read the action of a card: %w", err)
	}
	normal, ok := commandinterpreter.Normalize(op)
	if !ok {
		return actionResult{}, fmt.Errorf("the action %q of a card is not a valid operation", op.Type)
	}
	return s.applyOp(ctx, project, normal)
}

// applyOp carries op out through its handler.
func (s *DirectorService) applyOp(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (actionResult, error) {
	h, ok := s.handlerOf(op.Type)
	if !ok {
		if spec, registered := commandinterpreter.SpecOf(op.Type); registered && spec.Global {
			return actionResult{text: notWired, failed: true}, nil
		}
		return actionResult{}, fmt.Errorf("the action %q of a card is not an operation the chat carries out", op.Type)
	}
	var (
		res actionResult
		err error
	)
	switch {
	case h.chat != nil:
		var done chatops.Result
		done, err = h.chat.Apply(ctx, project, op)
		res = actionResult{text: done.Text, messages: done.Messages}
	case h.apply != nil:
		res.text, err = h.apply(s, ctx, project, op)
	default:
		return actionResult{}, fmt.Errorf("the action %q of a card is not an operation the chat carries out", op.Type)
	}
	var (
		ve  *ValidationError
		ref *chatops.Refusal
	)
	switch {
	case errors.As(err, &ve):
		return actionResult{text: "実行できませんでした: " + ve.Message, failed: true}, nil
	case errors.As(err, &ref):
		return actionResult{text: "実行できませんでした: " + ref.Message, failed: true}, nil
	}
	return res, err
}

// budgetAfter is the ceilings of the Project once op is applied: a ceiling the operation does not name stays, 0 removes it.
func (s *DirectorService) budgetAfter(ctx context.Context, projectID string, op commandinterpreter.Operation) (project, shot *float64, err error) {
	cur, err := s.policy.Policy(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	pick := func(named, current *float64) *float64 {
		switch {
		case named == nil:
			return current
		case *named == 0:
			return nil
		}
		return named
	}
	return pick(op.ProjectBudgetUSD, cur.Policy.ProjectBudgetCeiling), pick(op.ShotBudgetUSD, cur.Policy.ShotBudgetCeiling), nil
}

// askBudget is the sentence of the card that confirms a change of the budget ceilings: 「予算 $20 で進めますね？」. The amounts are checked as the settings screen
// checks them, so that a value that card would carry out is one the screen would take.
func (s *DirectorService) askBudget(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (string, error) {
	after, shotAfter, err := s.budgetAfter(ctx, project.ID, op)
	if err != nil {
		return "", err
	}
	if _, err := optionalUSD("project_budget", "Project budget", formUSD(after)); err != nil {
		return "", err
	}
	if _, err := optionalUSD("shot_budget", "Shot budget", formUSD(shotAfter)); err != nil {
		return "", err
	}
	cur, err := s.policy.Policy(ctx, project.ID)
	if err != nil {
		return "", err
	}
	if equalUSD(after, cur.Policy.ProjectBudgetCeiling) && equalUSD(shotAfter, cur.Policy.ShotBudgetCeiling) {
		return "", errNoChange
	}
	var question string
	switch {
	case op.ProjectBudgetUSD != nil && *op.ProjectBudgetUSD > 0 && op.ShotBudgetUSD == nil:
		question = "予算 " + dollars(after) + " で進めますね？"
	case op.ProjectBudgetUSD != nil && *op.ProjectBudgetUSD > 0 && op.ShotBudgetUSD != nil && *op.ShotBudgetUSD > 0:
		question = "予算 " + dollars(after) + "（1 Shot あたり " + dollars(shotAfter) + " まで）で進めますね？"
	case op.ProjectBudgetUSD == nil && op.ShotBudgetUSD != nil && *op.ShotBudgetUSD > 0:
		question = "1 Shot あたりの予算を " + dollars(shotAfter) + " にしますね？"
	default:
		question = "予算上限を次のように変えますね？\n・Project: " + dollars(after) + "\n・1 Shot あたり: " + dollars(shotAfter)
	}
	return question + "\n（いまの設定: Project " + dollars(cur.Policy.ProjectBudgetCeiling) + " / Shot " + dollars(cur.Policy.ShotBudgetCeiling) + "）" +
		"\n「" + confirmYes + "」を選ぶと設定します。設定画面の予算上限にも同じ値が入ります。", nil
}

// applyBudget sets the ceilings through the one path the settings screen and MCP use (PolicyService), so that the value in the chat is the value on the screen.
func (s *DirectorService) applyBudget(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (string, error) {
	projectCeiling, shotCeiling, err := s.budgetAfter(ctx, project.ID, op)
	if err != nil {
		return "", err
	}
	view, err := s.policy.setBudget(ctx, project.ID, BudgetForm{Project: formUSD(projectCeiling), Shot: formUSD(shotCeiling)}, false)
	if err != nil {
		return "", err
	}
	s.releaseBudgetStops(ctx, project.ID)
	text := "予算上限を更新しました（Project: " + dollars(view.Policy.ProjectBudgetCeiling) + " / Shot: " + dollars(view.Policy.ShotBudgetCeiling) + "）。設定画面の予算上限と同じ値です。"
	if view.Policy.ProjectBudgetCeiling == nil && view.Policy.ShotBudgetCeiling == nil {
		return text, nil
	}
	if notice := s.unpricedNotice(ctx, project.ID); notice != "" {
		text += "\n" + notice
	}
	return text, nil
}

// unpricedNotice is the sentence that tells the Project would use a model whose price is not known, which a budget ceiling cannot count; "" when every price is known.
func (s *DirectorService) unpricedNotice(ctx context.Context, projectID string) string {
	if s.unpriced == nil {
		return ""
	}
	models, err := s.unpriced(ctx, projectID)
	if err != nil {
		s.logger.Error("find the models of unknown price failed", "project_id", projectID, "err", err)
		return ""
	}
	if len(models) == 0 {
		return ""
	}
	return genplan.UnpricedModelsNotice(models)
}

// releaseBudgetStops gives back the starts the budget ceiling stopped, now that the ceiling or the policy that stopped them has changed: the next look of the
// Orchestrator tries them again. A failure is logged: what the user asked is done, and the stopped Shot can still be started by hand.
func (s *DirectorService) releaseBudgetStops(ctx context.Context, projectID string) {
	if err := s.genApprovals.ReleaseBudgetStops(ctx, projectID); err != nil {
		s.logger.Error("release the starts the budget stopped failed", "project_id", projectID, "err", err)
	}
}

func (s *DirectorService) applyQualityPolicy(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (string, error) {
	_, text, err := s.policy.setQualityPolicy(ctx, project.ID, op.Quality, nil)
	if err != nil {
		return "", err
	}
	s.releaseBudgetStops(ctx, project.ID)
	return text, nil
}

func (s *DirectorService) applyModeAction(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (string, error) {
	if !op.ProjectMode.Valid() {
		return "", invalid("project_mode", "MODE が正しくありません")
	}
	if cur, err := s.getProject(ctx, project.ID); err != nil {
		return "", err
	} else if cur.Mode == op.ProjectMode {
		return "すでに " + strings.ToUpper(string(cur.Mode)) + " MODE です。", nil
	}
	if err := s.applyMode(ctx, project.ID, op.ProjectMode, false); err != nil {
		return "", err
	}
	return approvalack.ModeNotice(op.ProjectMode), nil
}
