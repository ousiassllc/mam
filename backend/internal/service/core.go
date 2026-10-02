package service

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
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
	// pipeline drives the AUTO pipeline; nil when no Reasoning LLM is configured.
	pipeline *orchestrator.Orchestrator
	// afterAnswer lets the AUTO editing chain go on after the user changed something (see kick).
	afterAnswer func(projectID string)
	plans       *repository.PlanRepository
	// references is the Reference Library (F-11).
	references *repository.ReferenceRepository
	// director runs DIRECTOR MODE (F-02, F-24): the chat hands directions to it.
	director *DirectorService
	// continuer answers the replies to Continue cards; set by New.
	continuer *ContinueService
	// notes keeps the production notes and settles the promotion cards; set by New.
	notes *NoteService
	// branches are the Branches of the Project (F-17).
	branches *repository.BranchRepository
	// brandTemplates are the Brand Templates a new Project or Episode can start from (F-23, M3).
	brandTemplates *repository.BrandTemplateRepository
	// analytics records the human interventions the MVP Success Metrics count (spec/spec.md §28).
	analytics *repository.AnalyticsRepository
	// answering holds the Continue and Brief question cards that are being answered (card message
	// ID -> struct{}), so that a reply that overlaps another one to the same card is refused
	// instead of making a second Shot and a second paid generation, or putting the answer into
	// the next item of the Brief.
	answering sync.Map
	// repairable decides which Takes may be repaired (domain.CheckRepair); set by New.
	repairable repairability
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

// recordIntervention notes a hand operation of the user for the Production Analytics. It never
// fails the user's action: a missing row only makes the metrics a little too good.
func (c *core) recordIntervention(ctx context.Context, in domain.Intervention) {
	if err := c.analytics.Record(context.WithoutCancel(ctx), in); err != nil {
		c.logger.Error("record human intervention failed", "kind", in.Kind, "shot_id", in.ShotID, "take_id", in.TakeID, "err", err)
	}
}

// generationRecorder reads the state of the Shot before the user's generation of it is queued and
// returns what records that generation once it was queued: the first one is a generate, later ones
// are retakes, and asking again for a Shot the Judge passed is an override.
func (c *core) generationRecorder(ctx context.Context, shotID string) func() {
	hasTakes, passed, err := c.analytics.ShotState(ctx, shotID)
	if err != nil {
		c.logger.Error("read shot state for the analytics failed", "shot_id", shotID, "err", err)
		return func() {}
	}
	in := domain.Intervention{ShotID: shotID, Kind: domain.InterventionGenerate}
	if hasTakes {
		in.Kind, in.Override = domain.InterventionRegenerate, passed
	}
	return func() { c.recordIntervention(ctx, in) }
}

// kick lets the AUTO pipeline look at the Project after the user changed something
// (a finished Brief, an answered Approval card, a message after a failure). It never fails
// the user's action: the pipeline can be kicked again by the next change.
func (c *core) kick(ctx context.Context, projectID string) {
	if c.afterAnswer != nil {
		c.afterAnswer(projectID)
	}
	if c.pipeline == nil {
		return
	}
	if err := c.pipeline.Kick(context.WithoutCancel(ctx), projectID); err != nil {
		c.logger.Error("start the AUTO pipeline failed", "project_id", projectID, "err", err)
	}
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

// assetPath resolves a path stored in the database (relative to the assets directory, slash
// separated) to a file path inside the assets directory. It returns "" for an empty path and for a
// path that leaves the directory (a stored "../x"): callers treat "" as a file that is not there, so
// such a file is never read, served, probed or removed.
func (c *core) assetPath(rel string) string {
	if rel == "" || c.assetsDir == "" {
		return ""
	}
	abs := filepath.Join(c.assetsDir, filepath.FromSlash(rel))
	within, err := filepath.Rel(c.assetsDir, abs)
	if err != nil || within == "." || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return ""
	}
	return abs
}

// previewPath is the cache file name of a preview directly inside the preview directory. It returns
// "" when name (built from a row ID, which an imported bundle or a URL can make hostile) would put
// the file anywhere else.
func (c *core) previewPath(name string) string {
	abs := c.assetPath(previewDir + "/" + name)
	if abs == "" || filepath.Dir(abs) != filepath.Join(c.assetsDir, previewDir) {
		return ""
	}
	return abs
}
