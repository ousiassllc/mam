package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// MaxMessageRunes bounds one chat message.
const MaxMessageRunes = 4000

// core is what ProjectService and ChatService share.
type core struct {
	projects  *repository.ProjectRepository
	chat      *repository.ChatRepository
	assets    *repository.AssetRepository
	jobs      *repository.JobRepository
	hub       *Hub
	extractor briefing.Extractor
	assetsDir string
	logger    *slog.Logger
}

// getProject maps repository.ErrNotFound to ErrNotFound.
func (c *core) getProject(ctx context.Context, id string) (domain.Project, error) {
	p, err := c.projects.Get(ctx, id)
	return p, notFound(err)
}

func notFound(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// publishCreated tells the Project's clients that messages up to lastID exist.
func (c *core) publishCreated(projectID string, msgs []domain.ChatMessage) {
	if len(msgs) == 0 {
		return
	}
	c.hub.Publish(projectID, Event{Type: EventMessageCreated, MessageID: msgs[len(msgs)-1].ID})
}

// assignIDs gives every message an ID up front so callers know what was inserted (in order).
func assignIDs(msgs []domain.ChatMessage) []domain.ChatMessage {
	for i := range msgs {
		if msgs[i].ID == "" {
			msgs[i].ID = domain.NewID()
		}
	}
	return msgs
}

func assistantText(content string) domain.ChatMessage {
	return domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: content}
}

func userText(content, replyTo string, attachments []string) domain.ChatMessage {
	return domain.ChatMessage{
		Role: domain.RoleUser, Kind: domain.KindText, Content: content,
		ReplyToMessageID: replyTo, AttachmentAssetIDs: attachments,
	}
}
