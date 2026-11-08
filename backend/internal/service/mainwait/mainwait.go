// Package mainwait reads what the main line of a Project waits for after the Scene / Shot plan (#1108, domain/mainline).
package mainwait

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator/genstage"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Reader reads the Shots, the Reference Library and the World State of a Project.
type Reader struct {
	Plans      *repository.PlanRepository
	References *repository.ReferenceRepository
	World      *repository.WorldRepository
	// Gate starts the generation by itself; nil (or off): the main line waits for the user to start each Shot.
	Gate *genstage.Gate
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

// Wait is what the main line of the Project waits for: it is inactive unless the Project is in VISUAL_DESIGN (or waits at a card) with Shots that
// are PLANNING and have no Take. Phase says whether the Orchestrator carries the Shots on by itself, or holds them back for a card of v0.1.34 that was
// declined; Stuck counts the Shots it cannot carry on (they are started by hand).
func (r Reader) Wait(ctx context.Context, p domain.Project) (mainline.Wait, error) {
	if p.State != domain.StateVisualDesign && p.State != domain.StateAwaitingApproval {
		return mainline.Wait{}, nil
	}
	scenes, err := r.Plans.ListScenes(ctx, p.ID)
	if err != nil {
		return mainline.Wait{}, err
	}
	w := mainline.Wait{}
	var waiting []string
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
				waiting = append(waiting, sh.ID)
			}
		}
	}
	if w.Shots == 0 {
		return mainline.Wait{}, nil
	}
	phase, stuck, err := r.Gate.Phase(ctx, p)
	if err != nil {
		return mainline.Wait{}, err
	}
	w.Phase = phase
	if phase == mainline.PhaseAuto {
		for _, id := range waiting {
			if stuck[id] {
				w.Stuck++
			}
		}
	}
	if w.References, w.Characters, err = r.Facts(ctx, p.ID); err != nil {
		return mainline.Wait{}, err
	}
	w.Counted = true
	return w, nil
}
