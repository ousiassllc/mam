package service

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// removeAddedShots takes additional Shots that never started, and the Scenes made for them, out of the Project.
func (s *EditorService) removeAddedShots(ctx context.Context, reqs []repository.NewReshoot, sceneIDs []string) {
	ids := make([]string, len(reqs))
	for i, r := range reqs {
		ids[i] = r.ShotID
	}
	if err := s.plans.RemoveShots(ctx, ids, sceneIDs); err != nil {
		s.logger.Error("remove additional shots", "err", err)
	}
}
