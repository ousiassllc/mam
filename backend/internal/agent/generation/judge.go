package generation

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Progress vocabulary of the AI Video Judge, which reports on the same WebSocket (/ws/shots/:id).
const (
	StageJudging Stage  = "judging" // the AI Video Judge is watching a Take
	ActionJudge  Action = "judge"   // a job of the judge kind
)

// Judger starts the AI Video Judge on a Take. *judge.Runner implements it. The Orchestrator calls
// it for every Take it makes, so a Take is judged without a request from the user (spec §13).
type Judger interface {
	Start(ctx context.Context, takeID string) (domain.Job, error)
}
