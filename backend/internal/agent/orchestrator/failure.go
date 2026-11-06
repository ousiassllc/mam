package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
)

// failure is err as the common error (#1010): a plan the AI gave that cannot be used is the invalid output of the LLM; the failures of the
// LLM (no key, a timeout, a limit, the provider's own words) are errmap's.
func failure(err error) *apperr.Error {
	if errors.Is(err, planning.ErrInvalidPlan) {
		return apperr.New(apperr.CodeLLMInvalidOutput).Wrap(err)
	}
	return errmap.Explain(err)
}

// failureReason is the user-facing cause of a failure and what to do. Details stay in the log.
func failureReason(fail *apperr.Error) string {
	return fail.View(apperr.LangJa).Sentence(apperr.LangJa)
}

// advanceRecovered is advance that turns a panic into the error of the job: the user is told in the chat and the log line
// has the project_id, instead of the job record alone saying that the handler panicked (#1107).
func (o *Orchestrator) advanceRecovered(ctx context.Context, projectID string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			o.logger.Error("planning pipeline panicked", "project_id", projectID, "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("planning pipeline panicked: %v", r)
		}
	}()
	return o.advance(ctx, projectID)
}
