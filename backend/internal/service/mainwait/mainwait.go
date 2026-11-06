// Package mainwait reads what the main line of a Project waits for after the Scene / Shot plan (#1108, domain/mainline).
package mainwait

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Reader reads the Shots, the Reference Library and the World State of a Project.
type Reader struct {
	Plans      *repository.PlanRepository
	References *repository.ReferenceRepository
	World      *repository.WorldRepository
}

// Facts counts the Reference Library items and the Characters of the Project.
func (r Reader) Facts(ctx context.Context, projectID string) (references, characters int, err error) {
	items, err := r.References.List(ctx, projectID, repository.ReferenceFilter{})
	if err != nil {
		return 0, 0, err
	}
	w, err := r.World.World(ctx, projectID, "")
	if err != nil {
		return 0, 0, err
	}
	return len(items), len(w.Characters), nil
}

// Wait is what the main line of the Project waits for: it is inactive unless the Project is in VISUAL_DESIGN with Shots that are
// PLANNING and have no Take (no generation was started for them).
func (r Reader) Wait(ctx context.Context, p domain.Project) (mainline.Wait, error) {
	if p.State != domain.StateVisualDesign {
		return mainline.Wait{}, nil
	}
	scenes, err := r.Plans.ListScenes(ctx, p.ID)
	if err != nil {
		return mainline.Wait{}, err
	}
	w := mainline.Wait{}
	for _, sc := range scenes {
		shots, err := r.Plans.ListShots(ctx, sc.ID)
		if err != nil {
			return mainline.Wait{}, err
		}
		takes, err := r.Plans.ListTakes(ctx, sc.ID)
		if err != nil {
			return mainline.Wait{}, err
		}
		for _, sh := range shots {
			if sh.State == domain.ShotPlanning && len(takes[sh.ID]) == 0 {
				w.Shots++
			}
		}
	}
	if w.Shots == 0 {
		return mainline.Wait{}, nil
	}
	if w.References, w.Characters, err = r.Facts(ctx, p.ID); err != nil {
		return mainline.Wait{}, err
	}
	w.Counted = true
	return w, nil
}
