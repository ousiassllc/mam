package service

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

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
	slices.Sort(in.AttachmentAssetIDs) // the same asset is attached once
	in.AttachmentAssetIDs = slices.Compact(in.AttachmentAssetIDs)
	if len(in.AttachmentAssetIDs)+len(in.Uploads) > MaxAttachments {
		return Workspace{}, invalid("attachments", "添付は一度に %d 件までです", MaxAttachments)
	}
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
	// The direction was carried out (a regeneration may be queued and paid for): the answer is recorded
	// whatever happened to the request since, as DirectorCommand's is.
	if err := s.commit(context.WithoutCancel(ctx), repository.Turn{ProjectID: project.ID, Messages: msgs}); err != nil {
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
		if !domain.DecodeCardBody(target.Content).NonBlocking {
			resume := domain.EscalationResumeState
			turn.ResumeState = &resume
		}
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

	var refused *domain.ChatMessage
	if project.State == domain.StateDraft && len(brief.Missing()) > 0 {
		if refused, err = s.answerBrief(ctx, project.ID, &brief, target, replied, user.Content); err != nil {
			return turn, nil, err
		}
	}
	if refused != nil {
		turn.Messages = append(turn.Messages, *refused)
	}
	if !briefChanged(before, brief) {
		return turn, nil, nil
	}
	turn.Brief = &brief
	// The AI speaks again only when the answer moved the Brief forward (a refused answer asks again).
	beforeMissing, nowMissing := before.Missing(), brief.Missing()
	if refused == nil && !slices.Equal(beforeMissing, nowMissing) {
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

// answerBrief applies a message received while the Brief is incomplete; it returns the question again for an answer the Brief cannot hold.
func (s *ChatService) answerBrief(ctx context.Context, projectID string, brief *domain.Brief, target *domain.ChatMessage, replied map[string]bool, text string) (*domain.ChatMessage, error) {
	if target != nil && target.Kind == domain.KindQuestion && target.RefType == domain.RefBriefField && !replied[target.ID] {
		return applyAnswer(brief, domain.BriefField(target.RefID), text), nil
	}
	// Free input: take what the text says about the Brief, then treat the rest as the
	// answer to the newest open question.
	brief.Merge(s.extractor.Extract(text))
	open, err := s.chat.LatestOpenQuestion(ctx, projectID)
	if err != nil || open == nil {
		return nil, err
	}
	f := domain.BriefField(open.RefID)
	if brief.Get(f) != "" || brief.IsDelegated(f) {
		return nil, nil
	}
	return applyAnswer(brief, f, text), nil
}

// applyAnswer stores an answer; the Brief form's rule refuses it with the question again and the reason.
func applyAnswer(brief *domain.Brief, f domain.BriefField, text string) *domain.ChatMessage {
	text = strings.TrimSpace(text)
	if text == domain.DelegateOption {
		brief.Delegate(f)
	} else if problem := briefing.ValueProblem(f, text); problem != "" {
		again := briefing.Question(f, f.Label()+": "+problem+"\n"+f.Question())
		return &again
	} else if text != "" {
		brief.Set(f, text)
	}
	return nil
}

func briefChanged(a, b domain.Brief) bool {
	return !slices.Equal(a.Assets, b.Assets) || !slices.Equal(a.References, b.References) ||
		!slices.Equal(a.Delegated, b.Delegated) || a.AdditionalInstruction != b.AdditionalInstruction ||
		slices.ContainsFunc(domain.RequiredBriefFields(), func(f domain.BriefField) bool { return a.Get(f) != b.Get(f) })
}
