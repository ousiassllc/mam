// Package service is the application service layer. It implements use cases on
// top of agent and repository and is the only thing web/handler talks to.
package service

import (
	"log/slog"

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
}

// Services is the DI root handed to router.New. Each feature adds one field
// and one line in New; nothing else in this file needs to change.
type Services struct {
	Health      *HealthService
	Credentials *CredentialService
}

// New builds every service from deps.
func New(deps Deps) *Services {
	return &Services{
		Health:      NewHealthService(deps.DB),
		Credentials: NewCredentialService(deps.Credentials),
	}
}
