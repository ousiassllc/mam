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

// Judger runs the AI Video Judge on a Take. *judge.Runner implements it. The Orchestrator starts it
// for every Take it makes, so a Take is judged without a request from the user (spec §13), and
// evaluates Prompt Candidates in place because it needs their verdicts to pick the Best Take (§8).
type Judger interface {
	Start(ctx context.Context, takeID string) (domain.Job, error)
	// Evaluate judges the Take before it returns and returns the stored result.
	Evaluate(ctx context.Context, takeID string) (domain.JudgeResult, error)
}
