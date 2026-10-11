package service

import (
	"context"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// approveWords are the replies that count as an approval when the user answers an
// Approval card in words instead of pressing the button. Anything else is a request for changes.
var approveWords = []string{"承認", "承認します", "承認です", "ok", "okay", "はい", "いいね", "いいです", "問題ない", "問題なし", "lgtm", "お願いします"}

func (s *ChatService) planApprovalReply(project domain.Project, card, user domain.ChatMessage) repository.Turn {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(user.Content), "。.!！ "))
	decision := DecisionRequestChanges
	if slices.Contains(approveWords, normalized) {
		decision = DecisionApprove
	}
	return approvalTurn(project, card, decision, user)
}

// approvalTurn builds the turn that records the user's answer to card.
func approvalTurn(project domain.Project, card domain.ChatMessage, decision Decision, user domain.ChatMessage) repository.Turn {
	status, ack := domain.ApprovalApproved, "承認されました。次の工程に進みます。"
	if decision == DecisionRequestChanges {
		status, ack = domain.ApprovalChangesRequested, changesRequestedAck(card)
	}
	turn := repository.Turn{
		ProjectID: project.ID,
		Approval:  &repository.ApprovalUpdate{MessageID: card.ID, Status: status},
		Messages:  []domain.ChatMessage{user, assistantText(ack)},
	}
	if card.IsBlockingApproval() {
		next := resumeStateAfter(card, decision)
		turn.ResumeState = &next
		if decision == DecisionRequestChanges {
			turn.CheckpointChanges = true
		}
	}
	return turn
}

// changesRequestedAck tells what a request for changes does at the card's checkpoint, and no more:
// only the script is redone from the comment (the Writer reads the reply). A Rough Cut or Final sends
// the Project back to EDITING and nothing reads the comment or runs the Editor again, so the user is
// told to edit (spec/spec.md §4.4).
func changesRequestedAck(card domain.ChatMessage) string {
	cp, _ := domain.CheckpointForRefType(card.RefType)
	switch cp {
	case domain.CheckpointScript:
		return "修正依頼を受け付けました。コメントを反映して脚本を作り直します。"
	case domain.CheckpointRoughCut, domain.CheckpointFinal:
		return "修正依頼を受け付けました。EDITING に戻しました。コメントは自動では反映されないので、タイムラインエディタで編集してから進めてください。"
	}
	return "修正依頼を受け付けました。"
}

func resumeStateAfter(card domain.ChatMessage, decision Decision) domain.ProjectState {
	cp, known := domain.CheckpointForRefType(card.RefType)
	switch {
	case known && decision == DecisionApprove:
		return cp.ApprovedState()
	case known:
		return cp.ChangesRequestedState()
	case decision == DecisionApprove:
		return domain.StateGenerating
	}
	return domain.StatePlanning
}

// RespondApproval answers an Approval card with a button (POST /projects/:id/approvals/:messageId).
// The card is answered exactly once (ErrConflict otherwise). The Project leaves
// AWAITING_APPROVAL only when this was the last blocking card; Notes promotion cards
// never change the state. It returns the updated card.
func (s *ChatService) RespondApproval(ctx context.Context, projectID, messageID string, decision Decision, comment string) (domain.Project, domain.ThreadMessage, error) {
	if decision != DecisionApprove && decision != DecisionRequestChanges {
		return domain.Project{}, domain.ThreadMessage{}, invalid("decision", "approve または request_changes を指定してください")
	}
	comment = strings.TrimSpace(comment)
	if err := validateMessage(comment, true); err != nil {
		return domain.Project{}, domain.ThreadMessage{}, err
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return domain.Project{}, domain.ThreadMessage{}, err
	}
	card, err := s.chat.Get(ctx, projectID, messageID)
	if err != nil {
		return domain.Project{}, domain.ThreadMessage{}, notFound(err)
	}
	if card.Kind != domain.KindApproval {
		return domain.Project{}, domain.ThreadMessage{}, invalid("message", "承認カードではありません")
	}
	if card.ApprovalStatus != domain.ApprovalPending {
		return domain.Project{}, domain.ThreadMessage{}, ErrConflict
	}

	text := "承認しました。"
	if decision == DecisionRequestChanges {
		text = "修正を依頼しました。"
		if comment != "" {
			text = "修正を依頼: " + comment
		}
	} else if comment != "" {
		text = "承認しました。" + comment
	}
	turn := approvalTurn(project, card, decision, userText(text, card.ID, nil))
	if err := s.settlePromotion(ctx, card, &turn); err != nil {
		return domain.Project{}, domain.ThreadMessage{}, err
	}
	turn.Messages = assignIDs(turn.Messages)
	if err := s.commit(ctx, turn); err != nil {
		return domain.Project{}, domain.ThreadMessage{}, err
	}
	s.kick(ctx, projectID)
	s.hub.Publish(projectID, Event{Type: EventCardUpdated, MessageID: card.ID})
	return s.Card(ctx, projectID, card.ID)
}

// settlePromotion carries out the answer of a Notes promotion card (F-31): approving writes the
// proposal into the World State, requesting changes rejects it. The AI's answer in turn is replaced
// by what happened. Other cards are left alone.
func (s *ChatService) settlePromotion(ctx context.Context, card domain.ChatMessage, turn *repository.Turn) error {
	if card.RefType != domain.RefNotePromotion {
		return nil
	}
	decision := DecisionRequestChanges
	if turn.Approval != nil && turn.Approval.Status == domain.ApprovalApproved {
		decision = DecisionApprove
	}
	answer, err := s.notes.settlePromotion(ctx, card, decision)
	if err != nil {
		return err
	}
	last := len(turn.Messages) - 1
	turn.Messages[last].Content = answer
	return nil
}

// RequestApproval posts the Approval card of a checkpoint when the Confirmation Level
// asks for one (F-03) and holds the Project in AWAITING_APPROVAL until it is answered.
// summary describes the artifact; refID identifies it (script version, scene, ...).
// It reports whether a card was posted: false means the level lets the pipeline continue.
func (s *ChatService) RequestApproval(ctx context.Context, projectID string, cp domain.Checkpoint, summary, refID string) (bool, error) {
	if !cp.Valid() {
		return false, invalid("checkpoint", "確認ポイントが正しくありません: %s", cp)
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return false, err
	}
	if !project.ConfirmationLevel.RequiresApproval(cp, project.CustomCheckpoints) {
		return false, nil
	}
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: summary,
		RefType: cp.RefType(), RefID: refID, ApprovalStatus: domain.ApprovalPending,
	}
	return true, s.postBlocking(ctx, projectID, card)
}

// RaiseEscalation asks the user for a decision the AI cannot make (Retry Policy limit
// reached, ...). It is posted at every Confirmation Level, including FULL AUTO.
func (s *ChatService) RaiseEscalation(ctx context.Context, projectID, text string, options []string, shotID string) error {
	if strings.TrimSpace(text) == "" {
		return invalid("text", "相談内容が空です")
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return err
	}
	blocks := project.State.EscalationBlocks()
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindEscalation,
		Content: domain.EncodeCardBody(domain.CardBody{Text: text, Options: options, NonBlocking: !blocks}),
	}
	if shotID != "" {
		card.RefType, card.RefID = domain.RefShot, shotID
	}
	if !blocks {
		// A Repair of a Shot can end in an Escalation while the Project is past that stage (AUDIO /
		// READY / EXPORTED): the card is posted, but the Project keeps its state, as the Core State
		// Machine has no move from there to AWAITING_APPROVAL. The card is marked non-blocking, so
		// that it neither keeps a later approval from resuming the Project nor sends it to REPAIRING
		// when it is answered.
		return s.commit(ctx, repository.Turn{ProjectID: projectID, Messages: assignIDs([]domain.ChatMessage{card})})
	}
	return s.postBlocking(ctx, projectID, card)
}

func (s *ChatService) postBlocking(ctx context.Context, projectID string, card domain.ChatMessage) error {
	state := domain.StateAwaitingApproval
	return s.commit(ctx, repository.Turn{ProjectID: projectID, Messages: assignIDs([]domain.ChatMessage{card}), State: &state})
}

// Announce posts an AI message that does not block the Project: plain text, or a
// Question, Keyframe, Take or Progress card.
func (s *ChatService) Announce(ctx context.Context, projectID string, m domain.ChatMessage) error {
	switch m.Kind {
	case domain.KindText, domain.KindQuestion:
	case domain.KindKeyframe:
		m.RefType = domain.RefAsset
	case domain.KindTake:
		m.RefType = domain.RefTake
	case domain.KindProgress:
		m.RefType = domain.RefJob
	default:
		return invalid("kind", "%s カードは RequestApproval / RaiseEscalation で投稿してください", m.Kind)
	}
	if m.Kind != domain.KindText && m.Kind != domain.KindQuestion && m.RefID == "" {
		return invalid("ref_id", "%s カードには参照先が必要です", m.Kind)
	}
	m.Role = domain.RoleAssistant
	m.ApprovalStatus = ""
	return s.commit(ctx, repository.Turn{ProjectID: projectID, Messages: assignIDs([]domain.ChatMessage{m})})
}
