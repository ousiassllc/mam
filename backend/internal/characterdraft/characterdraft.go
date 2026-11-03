// Package characterdraft makes the drafts of the Characters of a Project from the images attached to its Brief, without an LLM (#1022). It
// is what is done when the planning stops for want of an LLM API key: the Project would otherwise have no Characters, and the images the
// user attached would not be used. A draft has a placeholder name ("登場人物 1"), the attached image as its face reference, and nothing
// else (what cannot be read from an image is left empty); it is marked Draft until the user edits it. It is a package of its own because
// service and repository are at their line limits.
package characterdraft

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/characterrepo"
)

// MaxImageBytes is the size above which an attached image is left out: service.MaxImageBytes, the limit of an upload (a test holds the two
// together; this package cannot import service, which wires it).
const MaxImageBytes = 20 << 20

// Maker makes the drafts.
type Maker struct {
	projects *repository.ProjectRepository
	assets   *repository.AssetRepository
	world    *repository.WorldRepository
	logger   *slog.Logger
}

// New returns a Maker. A nil logger discards its notes.
func New(projects *repository.ProjectRepository, assets *repository.AssetRepository, world *repository.WorldRepository, logger *slog.Logger) *Maker {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Maker{projects: projects, assets: assets, world: world, logger: logger}
}

// Result is what Make found and did.
type Result struct {
	// Created is the number of drafts Make added (0 when the Project had Characters already, or no usable image).
	Created int
	// Drafts is the number of drafts the Project has now (those of an earlier run included); it drops when the user edits one.
	Drafts int
	// Skipped says, for each attachment that was left out, why (not an image, over the size limit).
	Skipped []string
}

// Make adds a draft Character to the World State of the Project for each image attached to its Brief (an attachment that is not an
// image, or is over MaxImageBytes, is left out and told in Result.Skipped), in the order of the Brief, with the image as the face
// reference. It adds none when the World State already has a Character (the planning's, one made by hand, or a draft of an earlier run),
// so running it again does not add more. An unknown Project is repository.ErrNotFound.
func (m *Maker) Make(ctx context.Context, projectID string) (Result, error) {
	brief, err := m.projects.GetBrief(ctx, projectID)
	if err != nil {
		return Result{}, err
	}
	assets, err := m.assets.ListByIDs(ctx, projectID, brief.Assets)
	if err != nil {
		return Result{}, err
	}
	var res Result
	var rows []domain.WorldCharacter
	for _, a := range assets {
		switch {
		case a.Type != domain.AssetImage:
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s: 画像ではない添付です", a.ID))
		case a.SizeBytes > MaxImageBytes:
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s: サイズが上限（%d MB）を超えています", a.ID, MaxImageBytes>>20))
		default:
			rows = append(rows, domain.WorldCharacter{
				ID: domain.NewID(), Name: fmt.Sprintf("登場人物 %d", len(rows)+1), FaceReferenceAssetID: a.ID, Draft: true,
			})
		}
	}
	for _, s := range res.Skipped {
		m.logger.Info("character draft: attachment left out", "project_id", projectID, "reason", s)
	}
	added, err := characterrepo.DraftsInWorld(ctx, m.world, projectID, rows)
	if err != nil {
		return Result{}, err
	}
	if added {
		res.Created = len(rows)
	}
	w, err := m.world.World(ctx, projectID, "")
	if err != nil {
		return Result{}, err
	}
	for _, c := range w.Characters {
		if c.Draft {
			res.Drafts++
		}
	}
	return res, nil
}
