package repair_test

import (
	"context"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Every job that ends makes the loop look at the Shot (a generate job, a judge job, the repair job
// itself), so looks overlap. A look that read the Shot before a repair of it began must not start a
// second repair of the old Judge result once that repair is over: the user would be asked again about
// a result that is no longer the latest (#840).
func TestContinue_OverlappingLooksStartNoRepairOfAStaleJudgeResult(t *testing.T) {
	f := newFx(t) // balanced: 2 retries
	ctx := context.Background()
	stop := make(chan struct{})
	var looks sync.WaitGroup
	for range 3 {
		looks.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					f.agent.Continue(ctx, "sh1")
					runtime.Gosched()
				}
			}
		})
	}

	// The queue runs the jobs one at a time; a Take that a repair made is judged and fails like t1.
	judged := 0
	for handled := 0; handled < 3; handled++ { // t1, the first new Take, and the second new Take at the limit
		var job domain.Job
		require.Eventually(t, func() bool {
			jobs := f.jobs.enqueued()
			if len(jobs) <= handled {
				return false
			}
			job = jobs[handled]
			return true
		}, 10*time.Second, time.Millisecond, "the loop queues a repair")
		require.NoError(t, f.agent.Handle(ctx, job))
		f.regen.mu.Lock()
		made := slices.Clone(f.regen.made)
		f.regen.mu.Unlock()
		for ; judged < len(made); judged++ {
			f.judge(t, made[judged], failing(domain.JudgeIssue{Type: "camera_speed", Severity: domain.SeverityMedium}))
		}
	}
	close(stop)
	looks.Wait()
	for _, job := range f.jobs.enqueued()[3:] { // looks go on asking for a repair at the limit; it asks nothing new
		require.NoError(t, f.agent.Handle(ctx, job))
	}

	assert.Len(t, f.history(t), 2, "2 retries, no more")
	assert.Len(t, f.chat.escalations, 1, "the user is asked once")
}
