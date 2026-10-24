package projectlink

import (
	"context"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// ProjectRef is a Project the inputs or an import can name.
type ProjectRef struct {
	ID   string `json:"project_id"`
	Name string `json:"name"`
}

// ProjectList lists the Projects, newest first: the choices of "pick from a Project" and of an import.
func (l *Link) ProjectList(ctx context.Context) ([]ProjectRef, error) {
	list, err := l.Projects.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectRef, len(list))
	for i, p := range list {
		out[i] = ProjectRef{ID: p.ID, Name: p.Name}
	}
	return out, nil
}

// ProjectImage is an image of a Project that a batch can take as an input: an asset, with its Reference Library entry if it has one.
type ProjectImage struct {
	AssetID         string    `json:"asset_id"`
	ReferenceItemID string    `json:"reference_id,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	Name            string    `json:"name,omitempty"`
	MimeType        string    `json:"mime_type"`
	SizeBytes       int64     `json:"size_bytes"`
	CreatedAt       time.Time `json:"created_at"`
}

// ProjectImages lists the image assets of a Project, newest first (gterr.ErrNotFound for an unknown Project).
func (l *Link) ProjectImages(ctx context.Context, projectID string) ([]ProjectImage, error) {
	if _, err := l.Projects.Get(ctx, projectID); errors.Is(err, repository.ErrNotFound) {
		return nil, gterr.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	list, err := l.Repo.ProjectImages(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectImage, len(list))
	for i, p := range list {
		out[i] = ProjectImage{AssetID: p.AssetID, ReferenceItemID: p.ReferenceItemID, Kind: p.Kind, Name: p.Name, MimeType: p.MimeType, SizeBytes: p.SizeBytes, CreatedAt: p.CreatedAt}
	}
	return out, nil
}
