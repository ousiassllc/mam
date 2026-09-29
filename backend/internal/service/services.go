// Package service is the application service layer. It implements use cases on
// top of agent and repository and is the only thing web/handler talks to.
package service

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Deps are the infrastructure dependencies wired once in cmd/server.
type Deps struct {
	DB          *repository.DB
	Jobs        *jobqueue.Queue
	Credentials credential.Store
	Logger      *slog.Logger
	// AssetsDir is where uploaded files are stored (config.AssetsDir). Uploads fail when empty.
	AssetsDir string
	// Briefing extracts the Creative Brief from chat messages. Nil selects briefing.RuleExtractor.
	Briefing briefing.Extractor
}

// Services is the DI root handed to router.New. Each feature adds one field
// and one line in New; nothing else in this file needs to change.
type Services struct {
	Health      *HealthService
	Credentials *CredentialService
	Projects    *ProjectService
	Chat        *ChatService
}

// New builds every service from deps. When deps.Jobs is set, job status changes refresh
// the progress cards in the chat; New therefore MUST run before deps.Jobs.Start.
func New(deps Deps) *Services {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	extractor := deps.Briefing
	if extractor == nil {
		extractor = briefing.RuleExtractor{}
	}
	c := &core{
		projects:  repository.NewProjectRepository(deps.DB),
		chat:      repository.NewChatRepository(deps.DB),
		assets:    repository.NewAssetRepository(deps.DB),
		jobs:      repository.NewJobRepository(deps.DB),
		hub:       NewHub(),
		extractor: extractor,
		assetsDir: deps.AssetsDir,
		logger:    logger,
	}
	chat := &ChatService{c}
	if deps.Jobs != nil {
		deps.Jobs.OnChange(func(jobID string) { chat.JobChanged(context.Background(), jobID) })
	}
	return &Services{
		Health:      NewHealthService(deps.DB),
		Credentials: NewCredentialService(deps.Credentials),
		Projects:    &ProjectService{c},
		Chat:        chat,
	}
}
