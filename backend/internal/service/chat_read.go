package service

import (
	"context"

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
