package generation

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// updateWorldState runs the State Extractor on a Shot whose Takes have been made and appends the
// resulting World State version (F-10, spec/spec.md §9.3). takeID is the Take the State Diff is
// recorded against. The Takes already exist and cost money, so a failure is logged and never
// fails the generation: the World State then simply stays as it was and the next Shot starts
// from it.
func (o *Orchestrator) updateWorldState(ctx context.Context, ref repository.ShotRef, spec domain.ShotSpec, takeID string) {
	world, err := o.world.World(ctx, ref.ProjectID)
	if err != nil {
		o.logger.Warn("world state update skipped: read world state", "shot_id", ref.Shot.ID, "err", err)
		return
	}
	locks, err := o.world.Locks(ctx, ref.ProjectID)
	if err != nil {
		o.logger.Warn("world state update skipped: read locks", "shot_id", ref.Shot.ID, "err", err)
		return
	}
	diff, err := o.extractor.Extract(ctx, stateextractor.Input{Scene: ref.Scene, Shot: ref.Shot, Spec: spec, World: world, Locks: locks})
	if err != nil {
		o.logger.Warn("world state update skipped: state extractor failed", "shot_id", ref.Shot.ID, "err", err)
		return
	}
	// ApplyStateDiff judges the LOCKs again on the rows it writes.
	applied, version, err := o.world.ApplyStateDiff(ctx, ref.ProjectID, ref.Shot.ID, takeID, diff)
	if err != nil {
		o.logger.Warn("world state update failed", "shot_id", ref.Shot.ID, "err", err)
		return
	}
	o.logger.Info("world state updated", "shot_id", ref.Shot.ID, "version", version, "changes", len(applied.Changes))
}
