package chatbrief

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// OpenState is what decides whether a Question or Escalation card of the Project can still be answered.
type OpenState struct {
	Replied       map[string]bool
	Brief         domain.Brief
	State         domain.ProjectState
	LatestConfirm string // the ID of the newest summary card of the Brief
}

// LoadOpenState reads the OpenState of the Project.
func LoadOpenState(ctx context.Context, chat *repository.ChatRepository, projects *repository.ProjectRepository, projectID string) (OpenState, error) {
	var o OpenState
	var err error
	if o.LatestConfirm, err = LatestSummaryID(ctx, chat, projectID); err != nil {
		return o, err
	}
	if o.Replied, err = chat.RepliedMessageIDs(ctx, projectID); err != nil {
		return o, err
	}
	if o.Brief, err = projects.GetBrief(ctx, projectID); err != nil {
		return o, err
	}
	project, err := projects.Get(ctx, projectID)
	if err != nil {
		return o, err
	}
	o.State = project.State
	return o, nil
}
