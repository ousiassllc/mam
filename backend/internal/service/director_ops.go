package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing/budget"
	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/approvalack"
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
}

var opHandlers = map[commandinterpreter.OpType]opHandler{
	commandinterpreter.OpSetBudget:        {question: (*DirectorService).askBudget, apply: (*DirectorService).applyBudget},
	commandinterpreter.OpSetQualityPolicy: {apply: (*DirectorService).applyQualityPolicy},
	commandinterpreter.OpSetMode:          {apply: (*DirectorService).applyModeAction},
}

// errNoChange is the answer of a question that finds the operation already done.
var errNoChange = errors.New("service: the operation changes nothing")

// The buttons of a confirmation card, and the words that answer one in the composer.
const (
	confirmYes = "はい、この内容で進める"
	confirmNo  = "やめる"
)

var declineWords = []string{"やめる", "やめて", "やめます", "やめとく", "いいえ", "いや", "キャンセル", "違う", "ちがう", "不要", "no"}

// isDecline reports whether text, apart from surrounding blanks and 。.!！, is one of the words that decline.
func isDecline(text string) bool {
	return slices.Contains(declineWords, strings.ToLower(strings.Trim(strings.TrimSpace(text), "。.!！ ")))
}

// confirmCard builds the confirmation card of op. When the operation cannot be asked (it is invalid, or already done) the sentence to tell instead is returned and the
// card is empty.
func (s *DirectorService) confirmCard(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (domain.ChatMessage, string, error) {
	h, ok := opHandlers[op.Type]
	if !ok || h.question == nil {
		return domain.ChatMessage{}, "", fmt.Errorf("the operation %q asks for confirmation but has no question (opHandlers)", op.Type)
	}
	question, err := h.question(s, ctx, project, op)
	var ve *ValidationError
	switch {
	case errors.Is(err, errNoChange):
		return domain.ChatMessage{}, "すでにその内容です。変更しませんでした。", nil
	case errors.As(err, &ve):
		return domain.ChatMessage{}, "設定できませんでした: " + ve.Message, nil
	case err != nil:
		return domain.ChatMessage{}, "", err
	}
	action, err := json.Marshal(op)
	if err != nil {
		return domain.ChatMessage{}, "", err
	}
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindQuestion, RefType: domain.RefOperation, RefID: string(op.Type),
		Content: domain.EncodeCardBody(domain.CardBody{Text: question, Options: []string{confirmYes, confirmNo}, OptionActions: []json.RawMessage{action, nil}}),
	}
	return card, "", nil
}

// applyAction carries out the operation a card button carries (the JSON stored in CardBody.OptionActions). A reason the user can act on (an amount the budget
// refuses) is the sentence returned; any other failure is the error.
func (s *DirectorService) applyAction(ctx context.Context, project domain.Project, raw json.RawMessage) (string, error) {
	var op commandinterpreter.Operation
	if err := json.Unmarshal(raw, &op); err != nil {
		return "", fmt.Errorf("read the action of a card: %w", err)
	}
	if normal, ok := commandinterpreter.Normalize(op); ok {
		op = normal
	}
	h, ok := opHandlers[op.Type]
	if !ok || h.apply == nil {
		return "", fmt.Errorf("the action %q of a card is not an operation the chat carries out", op.Type)
	}
	text, err := h.apply(s, ctx, project, op)
	var ve *ValidationError
	if errors.As(err, &ve) {
		return "実行できませんでした: " + ve.Message, nil
	}
	return text, err
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

// budgetCards is what the chat answers when the director's message states a budget (the Brief extractor read it, or the interpreter returned a set_budget): the
// confirmation card, or the sentence that says why there is none.
func (s *DirectorService) budgetCards(ctx context.Context, project domain.Project, stated budget.Statement) ([]domain.ChatMessage, error) {
	if stated.Yen && !stated.Any() {
		return []domain.ChatMessage{assistantText("予算は USD（ドル）でしか設定できません。円での指定は換算しないので、「予算は $20 で」のようにドルで教えてください。")}, nil
	}
	op := commandinterpreter.Operation{Type: commandinterpreter.OpSetBudget, ProjectBudgetUSD: stated.Project, ShotBudgetUSD: stated.Shot}
	card, line, err := s.confirmCard(ctx, project, op)
	if err != nil {
		return nil, err
	}
	if line != "" {
		return []domain.ChatMessage{assistantText(line)}, nil
	}
	return []domain.ChatMessage{card}, nil
}

// formUSD writes a ceiling as the budget form takes it ("20", "12.5"); "" removes the ceiling.
func formUSD(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// dollars writes a ceiling the way the settings screen shows it, with the unit: "$20", "$12.5", "上限なし".
func dollars(v *float64) string {
	if v == nil {
		return "上限なし"
	}
	return "$" + formUSD(v)
}

func equalUSD(a, b *float64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
