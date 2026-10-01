package service

import (
	"context"
	"errors"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// VersionService lists and restores the versions of Reversible Decisions (F-28, spec/spec.md
// §29.4): Shot specs, World State (trunk and Branch) and Branches. A restore never deletes a
// version; it appends a copy of the chosen one, so the restore itself can be undone.
type VersionService struct {
	*core
	versions *repository.VersionRepository
	director *DirectorService
	world    *WorldService
}

// ParseVersionNumber reads the :versionNumber of a restore route. A number that is not a
// positive integer is a ValidationError.
func ParseVersionNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, invalid("versionNumber", "バージョン番号が正しくありません")
	}
	return n, nil
}

// RestoreShot restores the Shot to the spec of prompt version number and returns the refreshed
// Inspector (POST /shots/:id/versions/:versionNumber/restore). A LOCKed part that would change is
// ErrLocked, a Shot being generated is ErrBusy; nothing is written in either case.
func (s *VersionService) RestoreShot(ctx context.Context, shotID string, number int) (Inspector, error) {
	if _, err := s.versions.RestoreShotVersion(ctx, shotID, number); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return Inspector{}, ErrBusy
		}
		return Inspector{}, lockedError(notFound(err))
	}
	return s.director.Inspector(ctx, shotID)
}

// RestoreWorldState restores the World State of the Project's trunk (branchID "") or of the Branch
// to version number and returns the refreshed view of that same scope
// (POST /projects/:id/world-state/versions/:versionNumber/restore[?branch_id=B]). A LOCKed field
// that would change is ErrLocked and nothing is written.
func (s *VersionService) RestoreWorldState(ctx context.Context, projectID, branchID string, number int) (WorldView, error) {
	if _, err := s.versions.RestoreWorldStateVersion(ctx, projectID, branchID, number); err != nil {
		return WorldView{}, lockedOrNotFound(err)
	}
	return s.world.View(ctx, projectID, branchID)
}

func lockedOrNotFound(err error) error {
	if errors.Is(err, repository.ErrLocked) {
		return ErrLocked
	}
	return notFound(err)
}

// BranchHistory is the version history of one Branch: the versions of the Branch itself and of its
// own World State series, newest first.
type BranchHistory struct {
	ProjectID string
	BranchID  string
	Versions  []domain.VersionEntry
	World     []domain.VersionEntry
}

// Branch returns the version history of the Branch (GET /branches/:id/versions).
func (s *VersionService) Branch(ctx context.Context, branchID string) (BranchHistory, error) {
	projectID, err := s.versions.BranchProject(ctx, branchID)
	if err != nil {
		return BranchHistory{}, notFound(err)
	}
	h := BranchHistory{ProjectID: projectID, BranchID: branchID}
	if h.Versions, err = s.versions.BranchVersions(ctx, branchID); err != nil {
		return BranchHistory{}, notFound(err)
	}
	if h.World, err = s.versions.WorldStateVersions(ctx, projectID, branchID); err != nil {
		return BranchHistory{}, notFound(err)
	}
	return h, nil
}

// RestoreBranch restores the Branch itself (its name and World State reference) to version number
// (POST /branches/:id/versions/:versionNumber/restore) and returns the refreshed history.
func (s *VersionService) RestoreBranch(ctx context.Context, branchID string, number int) (BranchHistory, error) {
	if _, err := s.versions.RestoreBranchVersion(ctx, branchID, number); err != nil {
		return BranchHistory{}, notFound(err)
	}
	return s.Branch(ctx, branchID)
}

// RestoreBranchWorldState restores the Branch's own World State series to version number
// (POST /branches/:branchId/world-state/versions/:versionNumber/restore) and returns the
// refreshed history.
func (s *VersionService) RestoreBranchWorldState(ctx context.Context, branchID string, number int) (BranchHistory, error) {
	projectID, err := s.versions.BranchProject(ctx, branchID)
	if err != nil {
		return BranchHistory{}, notFound(err)
	}
	if _, err := s.versions.RestoreWorldStateVersion(ctx, projectID, branchID, number); err != nil {
		return BranchHistory{}, lockedOrNotFound(err)
	}
	return s.Branch(ctx, branchID)
}
