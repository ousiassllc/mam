package service

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ChatService runs the chat thread of a Project (F-30): the user's messages, the
// answers to cards, and the cards the pipeline posts.
type ChatService struct{ *core }

// Decision is the user's answer to an Approval card.
type Decision string

// Approval decisions (POST /projects/:id/approvals/:messageId).
const (
	DecisionApprove        Decision = "approve"
	DecisionRequestChanges Decision = "request_changes"
)

// Workspace is what the Chat Workspace page shows.
type Workspace struct {
	Project  domain.Project
	Messages []domain.ThreadMessage
	// Updated are earlier cards that the returned messages answered (a question or
	// approval replied to in words); the client replaces them in place.
	Updated []domain.ThreadMessage
}

// Workspace returns the Project with its whole thread (F-30: history is shown again on reopen).
func (s *ChatService) Workspace(ctx context.Context, projectID string) (Workspace, error) {
	return s.Messages(ctx, projectID, "")
}

// Messages returns the messages after the given message ID ("" = the whole thread)
// with the Project (its state is shown next to the thread).
func (s *ChatService) Messages(ctx context.Context, projectID, after string) (Workspace, error) {
	msgs, err := s.chat.List(ctx, projectID, after)
	if err != nil {
		return Workspace{}, err
	}
	return s.workspaceOf(ctx, projectID, msgs)
}

// workspaceOf renders the given messages together with the current Project state and
// the answered cards among their reply targets.
func (s *ChatService) workspaceOf(ctx context.Context, projectID string, msgs []domain.ChatMessage) (Workspace, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return Workspace{}, err
	}
	views, err := s.views(ctx, projectID, msgs)
	if err != nil {
		return Workspace{}, err
	}
	ws := Workspace{Project: project, Messages: views}

	shown := make(map[string]bool, len(views))
	for _, v := range views {
		shown[v.ID] = true
	}
	var targets []domain.ChatMessage
	for _, v := range views {
		if p := v.ReplyTo; p != nil && !shown[p.ID] && isAnswerable(p.Kind) {
			shown[p.ID] = true
			targets = append(targets, *p)
		}
	}
	if ws.Updated, err = s.views(ctx, projectID, targets); err != nil {
		return Workspace{}, err
	}
	return ws, nil
}

// isBriefQuestion reports whether m is the question card of a Brief item.
func isBriefQuestion(m domain.ChatMessage) bool {
	return m.Kind == domain.KindQuestion && m.RefType == domain.RefBriefField
}

func isAnswerable(k domain.MessageKind) bool {
	return k == domain.KindQuestion || k == domain.KindEscalation || k == domain.KindApproval
}

// Card returns one message (a card that changed in place) with its Project.
func (s *ChatService) Card(ctx context.Context, projectID, messageID string) (domain.Project, domain.ThreadMessage, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return domain.Project{}, domain.ThreadMessage{}, err
	}
	m, err := s.chat.Get(ctx, projectID, messageID)
	if err != nil {
		return domain.Project{}, domain.ThreadMessage{}, notFound(err)
	}
	views, err := s.views(ctx, projectID, []domain.ChatMessage{m})
	if err != nil {
		return domain.Project{}, domain.ThreadMessage{}, err
	}
	return project, views[0], nil
}

// Subscribe streams the Project's events (WebSocket /ws/projects/:id).
func (s *ChatService) Subscribe(ctx context.Context, projectID string) (<-chan Event, func(), error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return nil, nil, err
	}
	ch, cancel := s.hub.Subscribe(projectID)
	return ch, cancel, nil
}

// JobChanged refreshes the progress cards of a job. The job queue calls it on every status change.
func (s *ChatService) JobChanged(ctx context.Context, jobID string) {
	msgs, err := s.chat.ListByRef(ctx, domain.RefJob, jobID)
	if err != nil {
		s.logger.Error("find progress cards failed", "job_id", jobID, "err", err)
		return
	}
	for _, m := range msgs {
		s.hub.Publish(m.ProjectID, Event{Type: EventCardUpdated, MessageID: m.ID})
	}
}

// TakeJudged refreshes the Take cards of a Take after a Judge result was saved: the card draws the
// Issues of the latest result as markers on the player and has to be fetched again to show them.
func (s *ChatService) TakeJudged(ctx context.Context, takeID string) {
	msgs, err := s.chat.ListByRef(ctx, domain.RefTake, takeID)
	if err != nil {
		s.logger.Error("find take cards failed", "take_id", takeID, "err", err)
		return
	}
	for _, m := range msgs {
		s.hub.Publish(m.ProjectID, Event{Type: EventCardUpdated, MessageID: m.ID})
	}
}

// PostMessageInput is one user message (POST /projects/:id/messages).
type PostMessageInput struct {
	Content            string
	ReplyToMessageID   string
	AttachmentAssetIDs []string // existing assets of the Project
	Uploads            []Upload // new files, stored as assets
}

// PostMessage appends the user's message and the AI's answer to the thread and returns
// both for rendering. While the Brief is incomplete the message answers the open
// Question (a reply to its card, or free input); a reply to an Approval card answers it;
// a reply to an Escalation card resumes the Project.
func (s *ChatService) PostMessage(ctx context.Context, projectID string, in PostMessageInput) (Workspace, error) {
	content := strings.TrimSpace(in.Content)
	if err := validateMessage(content, len(in.Uploads)+len(in.AttachmentAssetIDs) > 0); err != nil {
		return Workspace{}, err
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return Workspace{}, err
	}
	var target *domain.ChatMessage
	if in.ReplyToMessageID != "" {
		m, err := s.chat.Get(ctx, projectID, in.ReplyToMessageID)
		if errors.Is(err, repository.ErrNotFound) {
			return Workspace{}, invalid("reply_to_message_id", "返信先のメッセージが見つかりません")
		}
		if err != nil {
			return Workspace{}, err
		}
		target = &m
	}
	if target != nil && target.Kind == domain.KindQuestion && target.RefType == domain.RefContinue {
		return s.answerContinue(ctx, project, *target, content, len(in.Uploads)+len(in.AttachmentAssetIDs) > 0)
	}
	if target != nil && isBriefQuestion(*target) {
		// A card takes one answer at a time: the reply that arrives while another one to the same
		// card is being recorded (a double click) is a conflict, like a reply to an answered card.
		if _, busy := s.answering.LoadOrStore(target.ID, struct{}{}); busy {
			return Workspace{}, ErrConflict
		}
		defer s.answering.Delete(target.ID)
	}
	existing, err := s.assets.ListByIDs(ctx, projectID, in.AttachmentAssetIDs)
	if err != nil {
		return Workspace{}, err
	}
	if len(existing) != len(in.AttachmentAssetIDs) {
		return Workspace{}, invalid("attachment_asset_ids", "添付ファイルが見つかりません")
	}
	stored, err := s.storeUploads(projectID, in.Uploads)
	if err != nil {
		return Workspace{}, err
	}
	attached := slices.Concat(existing, assetsOf(stored))
	user := userText(content, in.ReplyToMessageID, assetIDs(attached))

	if s.director.wants(project, target, content) {
		return s.postDirection(ctx, project, target, user, stored, attached)
	}
	turn, decided, err := s.planTurn(ctx, project, target, user, attached)
	if err != nil {
		discardUploads(stored)
		return Workspace{}, err
	}
	turn.Messages = assignIDs(turn.Messages)
	turn.Assets = assetsOf(stored)
	if err := s.commit(ctx, turn); err != nil {
		discardUploads(stored)
		return Workspace{}, err
	}
	s.kick(ctx, projectID)
	ws, err := s.workspaceOf(ctx, projectID, turn.Messages)
	if err != nil {
		return Workspace{}, err
	}
	return s.withAnsweredQuestions(ctx, ws, decided)
}

// postDirection handles a message of DIRECTOR MODE: it is stored first, then the Command Interpreter
// reads it, resolves the Shot it is about and carries it out, and the AI's answer (what was done,
// refused or asked) follows it in the thread.
func (s *ChatService) postDirection(ctx context.Context, project domain.Project, target *domain.ChatMessage, user domain.ChatMessage, stored []storedUpload, attached []domain.Asset) (Workspace, error) {
	user.ID = domain.NewID()
	turn := repository.Turn{ProjectID: project.ID, Messages: []domain.ChatMessage{user}, Assets: assetsOf(stored)}
	if err := s.attachToBrief(ctx, &turn, attached); err != nil {
		discardUploads(stored)
		return Workspace{}, err
	}
	if err := s.commit(ctx, turn); err != nil {
		discardUploads(stored)
		return Workspace{}, err
	}
	reply, err := s.director.Chat(ctx, project, target, user.Content)
	if err != nil {
		return Workspace{}, err
	}
	msgs := assignIDs([]domain.ChatMessage{reply})
	if err := s.commit(ctx, repository.Turn{ProjectID: project.ID, Messages: msgs}); err != nil {
		return Workspace{}, err
	}
	return s.workspaceOf(ctx, project.ID, append([]domain.ChatMessage{user}, msgs...))
}

// withAnsweredQuestions adds the question cards of the Brief fields this turn decided:
// a free-text answer closes them although the user did not reply to their card.
func (s *ChatService) withAnsweredQuestions(ctx context.Context, ws Workspace, fields []domain.BriefField) (Workspace, error) {
	if len(fields) == 0 {
		return ws, nil
	}
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = string(f)
	}
	questions, err := s.chat.ListBriefQuestions(ctx, ws.Project.ID, names)
	if err != nil {
		return Workspace{}, err
	}
	shown := make(map[string]bool)
	for _, m := range ws.Messages {
		shown[m.ID] = true
	}
	for _, m := range ws.Updated {
		shown[m.ID] = true
	}
	var extra []domain.ChatMessage
	for _, q := range questions {
		if !shown[q.ID] {
			extra = append(extra, q)
		}
	}
	views, err := s.views(ctx, ws.Project.ID, extra)
	if err != nil {
		return Workspace{}, err
	}
	ws.Updated = append(ws.Updated, views...)
	return ws, nil
}

// commit persists a turn and tells the clients about the new messages.
func (s *ChatService) commit(ctx context.Context, turn repository.Turn) error {
	if err := s.projects.Apply(ctx, turn); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return ErrConflict
		}
		return notFound(err)
	}
	s.publishCreated(turn.ProjectID, turn.Messages)
	if turn.CheckpointChanges {
		s.recordIntervention(ctx, domain.Intervention{ProjectID: turn.ProjectID, Kind: domain.InterventionRequestChanges, Override: true})
	}
	return nil
}

// planTurn decides what the user's message means and what the AI answers. It also
// returns the Brief fields the message decided, whose question cards are now closed.
func (s *ChatService) planTurn(ctx context.Context, project domain.Project, target *domain.ChatMessage, user domain.ChatMessage, attached []domain.Asset) (repository.Turn, []domain.BriefField, error) {
	turn := repository.Turn{ProjectID: project.ID, Messages: []domain.ChatMessage{user}}
	if target != nil && target.Kind == domain.KindApproval && target.ApprovalStatus == domain.ApprovalPending {
		turn := s.planApprovalReply(project, *target, user)
		if err := s.settlePromotion(ctx, *target, &turn); err != nil {
			return turn, nil, err
		}
		if err := s.attachToBrief(ctx, &turn, attached); err != nil {
			return turn, nil, err
		}
		return turn, nil, nil
	}

	replied, err := s.chat.RepliedMessageIDs(ctx, project.ID)
	if err != nil {
		return turn, nil, err
	}
	if target != nil && isBriefQuestion(*target) && replied[target.ID] {
		// Answered already: applying the text again would put it into the next open item. An
		// Approval card is refused the same way (RespondApproval).
		return turn, nil, ErrConflict
	}
	if target != nil && target.Kind == domain.KindEscalation && !replied[target.ID] {
		resume := domain.EscalationResumeState
		turn.ResumeState = &resume
		turn.Messages = append(turn.Messages, assistantText("判断を受け付けました。この判断は自動では実行されません。内容に沿って、設定の変更・Take の採用・Shot の指示の修正などを画面から行ってください。"))
		if err := s.attachToBrief(ctx, &turn, attached); err != nil {
			return turn, nil, err
		}
		return turn, nil, nil
	}

	brief, err := s.projects.GetBrief(ctx, project.ID)
	if err != nil {
		return turn, nil, notFound(err)
	}
	before := brief
	brief.AttachAssets(attached)

	if project.State == domain.StateDraft && len(brief.Missing()) > 0 {
		if err := s.answerBrief(ctx, project.ID, &brief, target, replied, user.Content); err != nil {
			return turn, nil, err
		}
	}
	if !briefChanged(before, brief) {
		return turn, nil, nil
	}
	turn.Brief = &brief
	// The AI speaks again only when the answer moved the Brief forward.
	beforeMissing, nowMissing := before.Missing(), brief.Missing()
	if !slices.Equal(beforeMissing, nowMissing) {
		turn.Messages = append(turn.Messages, briefFollowUp(brief))
	}
	decided := slices.DeleteFunc(slices.Clone(beforeMissing), func(f domain.BriefField) bool { return slices.Contains(nowMissing, f) })
	return turn, decided, nil
}

// attachToBrief puts the files a message carries into the Brief (images into Assets, videos into
// References) in the same turn, whatever the message answers: a direction, an Approval card or an
// Escalation. A message that answers a question of the Brief takes them in planTurn instead.
func (s *ChatService) attachToBrief(ctx context.Context, turn *repository.Turn, attached []domain.Asset) error {
	if len(attached) == 0 {
		return nil
	}
	brief, err := s.projects.GetBrief(ctx, turn.ProjectID)
	if err != nil {
		return notFound(err)
	}
	before := brief
	brief.AttachAssets(attached)
	if briefChanged(before, brief) {
		turn.Brief = &brief
	}
	return nil
}

// answerBrief applies a message received while the Brief is incomplete.
func (s *ChatService) answerBrief(ctx context.Context, projectID string, brief *domain.Brief, target *domain.ChatMessage, replied map[string]bool, text string) error {
	if target != nil && target.Kind == domain.KindQuestion && target.RefType == domain.RefBriefField && !replied[target.ID] {
		applyAnswer(brief, domain.BriefField(target.RefID), text)
		return nil
	}
	// Free input: take what the text says about the Brief, then treat the rest as the
	// answer to the newest open question.
	brief.Merge(s.extractor.Extract(text))
	open, err := s.chat.LatestOpenQuestion(ctx, projectID)
	if err != nil {
		return err
	}
	if open != nil {
		f := domain.BriefField(open.RefID)
		if brief.Get(f) == "" && !brief.IsDelegated(f) {
			applyAnswer(brief, f, text)
		}
	}
	return nil
}

func applyAnswer(brief *domain.Brief, f domain.BriefField, text string) {
	text = strings.TrimSpace(text)
	if text == domain.DelegateOption {
		brief.Delegate(f)
		return
	}
	if text != "" {
		brief.Set(f, text)
	}
}

func briefChanged(a, b domain.Brief) bool {
	return !slices.Equal(a.Assets, b.Assets) || !slices.Equal(a.References, b.References) ||
		!slices.Equal(a.Delegated, b.Delegated) || a.AdditionalInstruction != b.AdditionalInstruction ||
		slices.ContainsFunc(domain.RequiredBriefFields(), func(f domain.BriefField) bool { return a.Get(f) != b.Get(f) })
}

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
		status, ack = domain.ApprovalChangesRequested, "修正依頼を受け付けました。内容を反映してやり直します。"
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
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindEscalation,
		Content: domain.EncodeCardBody(domain.CardBody{Text: text, Options: options}),
	}
	if shotID != "" {
		card.RefType, card.RefID = domain.RefShot, shotID
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return err
	}
	if !project.State.CanAwaitApproval() {
		// A Repair of a Shot can end in an Escalation while the Project is past that stage (AUDIO /
		// READY / EXPORTED): the card is posted, but the Project keeps its state, as the Core State
		// Machine has no move from there to AWAITING_APPROVAL and the reply (ResumeState) only
		// resumes a Project that is waiting.
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

// views prepares messages for rendering: attachments, reply previews, whether a
// card can still be answered, and the live job of progress cards.
func (s *ChatService) views(ctx context.Context, projectID string, msgs []domain.ChatMessage) ([]domain.ThreadMessage, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	var attachmentIDs, jobIDs []string
	byID := make(map[string]domain.ChatMessage, len(msgs))
	needsAnswer := false
	for _, m := range msgs {
		byID[m.ID] = m
		attachmentIDs = append(attachmentIDs, m.AttachmentAssetIDs...)
		needsAnswer = needsAnswer || m.Kind == domain.KindQuestion || m.Kind == domain.KindEscalation
		if m.Kind == domain.KindProgress {
			jobIDs = append(jobIDs, m.RefID)
		}
	}
	var parentIDs []string // reply targets outside this batch (the batch is the whole thread, or only what is new)
	for _, m := range msgs {
		if _, ok := byID[m.ReplyToMessageID]; m.ReplyToMessageID != "" && !ok {
			parentIDs = append(parentIDs, m.ReplyToMessageID)
		}
	}
	parents, err := s.chat.ListByIDs(ctx, projectID, parentIDs)
	if err != nil {
		return nil, err
	}
	jobs, err := s.jobs.ListByIDs(ctx, jobIDs)
	if err != nil {
		return nil, err
	}
	assets, err := s.assets.ListByIDs(ctx, projectID, attachmentIDs)
	if err != nil {
		return nil, err
	}
	assetByID := make(map[string]domain.Asset, len(assets))
	for _, a := range assets {
		assetByID[a.ID] = a
	}

	var replied map[string]bool
	var brief domain.Brief
	if needsAnswer {
		if replied, err = s.chat.RepliedMessageIDs(ctx, projectID); err != nil {
			return nil, err
		}
		if brief, err = s.projects.GetBrief(ctx, projectID); err != nil {
			return nil, notFound(err)
		}
	}

	views := make([]domain.ThreadMessage, len(msgs))
	for i, m := range msgs {
		v := domain.ThreadMessage{ChatMessage: m}
		for _, id := range m.AttachmentAssetIDs {
			if a, ok := assetByID[id]; ok {
				v.Attachments = append(v.Attachments, a)
			}
		}
		if m.ReplyToMessageID != "" {
			parent, ok := byID[m.ReplyToMessageID]
			if !ok {
				if parent, ok = parents[m.ReplyToMessageID]; !ok {
					return nil, notFound(repository.ErrNotFound)
				}
			}
			v.ReplyTo = &parent
		}
		switch m.Kind {
		case domain.KindQuestion:
			f := domain.BriefField(m.RefID)
			if m.RefType == domain.RefContinue {
				v.Open = !replied[m.ID]
			} else {
				v.Open = !replied[m.ID] && brief.Get(f) == "" && !brief.IsDelegated(f)
			}
		case domain.KindEscalation:
			v.Open = !replied[m.ID]
		case domain.KindProgress:
			if job, ok := jobs[m.RefID]; ok {
				v.Job = &job
			}
		}
		views[i] = v
	}
	return views, nil
}
