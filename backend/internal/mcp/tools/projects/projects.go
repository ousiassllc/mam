// Package projects holds the MCP tools of the Project list and Project creation (the home screen).
package projects

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "list_projects",
		"プロジェクトの一覧（新しい順）/ List the Projects, newest first. 画面のホームと同じ内容。",
		listProjects(svc.Projects))
}

// ListProjectsInput has no field: the list is not filtered.
type ListProjectsInput struct{}

// ProjectSummary is one Project in the list.
type ProjectSummary struct {
	ID                string    `json:"id"`
	Name              string    `json:"name" jsonschema:"表示名 / display name"`
	State             string    `json:"state" jsonschema:"Core State Machine の状態（DRAFT, PLANNING, GENERATING, EDITING, READY ...）"`
	Mode              string    `json:"mode" jsonschema:"auto or director"`
	ConfirmationLevel string    `json:"confirmation_level" jsonschema:"full_auto, key_check or custom_check"`
	SeriesID          string    `json:"series_id,omitempty"`
	EpisodeNumber     int       `json:"episode_number,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// ListProjectsOutput is the Projects, newest first.
type ListProjectsOutput struct {
	Projects []ProjectSummary `json:"projects"`
}

func listProjects(svc *service.ProjectService) mcpkit.Handler[ListProjectsInput, ListProjectsOutput] {
	return func(ctx context.Context, _ ListProjectsInput) (ListProjectsOutput, error) {
		list, err := svc.List(ctx)
		if err != nil {
			return ListProjectsOutput{}, err
		}
		out := ListProjectsOutput{Projects: make([]ProjectSummary, 0, len(list))}
		for _, p := range list {
			out.Projects = append(out.Projects, summary(p))
		}
		return out, nil
	}
}

func summary(p domain.Project) ProjectSummary {
	return ProjectSummary{
		ID: p.ID, Name: p.Name, State: string(p.State), Mode: string(p.Mode),
		ConfirmationLevel: string(p.ConfirmationLevel), SeriesID: p.SeriesID,
		EpisodeNumber: p.EpisodeNumber, CreatedAt: p.CreatedAt,
	}
}
