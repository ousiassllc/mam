// Package projectlink is the part of the generation test that touches a Project: the Projects and images a batch can take as
// inputs, and the import of a result into a Project (as a Reference Library entry or an asset, optionally assigned to a slot).
// Package gentest embeds a Link, so the service answers these calls.
package projectlink

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// AssignFunc puts the Reference Library image of a Project on a slot of a Character, Product or Location
// (service.WorldService.AssignReference). Its errors (locked, wrong kind, not found) are returned by Import as they are.
type AssignFunc func(ctx context.Context, projectID string, target domain.WorldTarget, id, slot, referenceID string) error

// Deps are what a Link needs.
type Deps struct {
	// Repo is the generation test's tables; Dir the directory of its files (<data-dir>/gentest).
	Repo *gentestrepo.Repository
	Dir  string
	// AssetsDir is where an imported file is stored (config.AssetsDir).
	AssetsDir  string
	Projects   *repository.ProjectRepository
	References *repository.ReferenceRepository
	// Assign is the optional second step of an import (nil: an import with a slot is refused).
	Assign AssignFunc
	Now    func() time.Time
	Logger *slog.Logger
}

// Link is the Project side of the generation test.
type Link struct{ Deps }

// New returns the Link. Now and Logger default (the wall clock, no log).
func New(d Deps) *Link {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	return &Link{Deps: d}
}

func (l *Link) filePath(relative string) string {
	return filepath.Join(l.Dir, filepath.FromSlash(relative))
}
