package stages

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
)

// mainLineWait is what the main line waits for once shots Shots are planned: the generation approval that is posted next, or (the gate is off)
// the user's start of each Shot. A failure to count the Library and the Characters is logged and leaves them out of the sentence: the wait reason
// is told either way.
func (s *Pipeline) mainLineWait(ctx context.Context, projectID string, shots int) mainline.Wait {
	wait := mainline.Wait{Shots: shots}
	if s.gate.On() {
		wait.Phase = mainline.PhaseApproval
	}
	if s.facts == nil {
		return wait
	}
	refs, chars, err := s.facts(ctx, projectID)
	if err != nil {
		s.logger.Error("count references and characters failed", "project_id", projectID, "err", err)
		return wait
	}
	wait.Counted, wait.References, wait.Characters = true, refs, chars
	return wait
}
