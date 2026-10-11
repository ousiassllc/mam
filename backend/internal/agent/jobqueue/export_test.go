package jobqueue

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// SetClaim replaces the function workers use to claim the next job.
func (q *Queue) SetClaim(fn func(ctx context.Context) (domain.Job, error)) { q.claim = fn }

// ClaimFunc returns the function workers currently use to claim the next job.
func (q *Queue) ClaimFunc() func(ctx context.Context) (domain.Job, error) { return q.claim }

// SetClaimBackoff sets the initial wait after a failed claim.
func (q *Queue) SetClaimBackoff(d time.Duration) { q.claimBackoff = d }
