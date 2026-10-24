package servicetest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

func TestCancelStopsDispatchingAndLetsInFlightCallsFinish(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		v := c.Vendors[modeladapter.VendorFlux]
		v.MaxConcurrency = 1
		c.Vendors[modeladapter.VendorFlux] = v
	}))
	release := r.Fake.Hold(domain.ModelFlux2)
	req := imageReq("flux-2")
	req.Count = 3
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	<-r.Fake.Started() // the first FLUX.2 call is in flight; the vendor allows one at a time

	res, err := r.Svc.Cancel(context.Background(), ref.BatchID)
	require.NoError(t, err)
	assert.Equal(t, gentest.CancelResult{Cancelled: 2, Running: 1}, res, "the call that is in flight is not cut off")
	release <- struct{}{}
	st := r.Wait(ref.BatchID)

	assert.Equal(t, gentest.StateCancelled, st.State)
	assert.Equal(t, 1, st.Succeeded, "the call that was in flight finished and kept its result")
	assert.Equal(t, 2, st.Cancelled)
	for _, x := range st.Results {
		if x.Status == gentestrepo.StatusCancelled {
			assert.Equal(t, gentest.ReasonCancelled, x.Error)
			assert.False(t, x.HasFile)
		}
	}
	assert.Len(t, r.Fake.CallsOf(domain.ModelFlux2), 1, "no further FLUX.2 call was dispatched")
	assert.Equal(t, st.Total, st.Done)

	_, err = r.Svc.Cancel(context.Background(), ref.BatchID)
	var c *gterr.ConflictError
	assert.ErrorAs(t, err, &c, "a finished batch cannot be cancelled")
	_, err = r.Svc.Cancel(context.Background(), "nope")
	assert.ErrorIs(t, err, gterr.ErrNotFound)
}

func TestProgressIsCountedFromTheResults(t *testing.T) {
	r := gentesttest.New(t)
	release := r.Fake.Hold(domain.ModelFlux2)
	req := imageReq("nano-banana", "flux-2")
	req.Count = 2
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	<-r.Fake.Started()
	require.Eventually(t, func() bool {
		st, err := r.Svc.Batch(context.Background(), ref.BatchID, false)
		require.NoError(t, err)
		return st.Succeeded == 2 && st.Running >= 1
	}, 10*time.Second, 5*time.Millisecond)

	st, err := r.Svc.BatchOfJob(context.Background(), ref.JobID, true)
	require.NoError(t, err)
	assert.Equal(t, gentest.StateRunning, st.State)
	assert.Equal(t, 4, st.Total)
	assert.Equal(t, 2, st.Done)
	assert.False(t, st.Finished)

	release <- struct{}{}
	release <- struct{}{}
	st = r.Wait(ref.BatchID)
	assert.Equal(t, 4, st.Done)
	_, err = r.Svc.BatchOfJob(context.Background(), "no-such-job", false)
	assert.ErrorIs(t, err, gterr.ErrNotFound)
}

func TestOnStartupFailsWhatTheCrashLeftAndKeepsWhatFinished(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	// A batch as a crashed mam left it: one result done, one running, one waiting.
	nb := gentestrepo.NewBatch{Batch: gentestrepo.Batch{ID: "crashed", Media: "image", CreatedAt: time.Now().UTC()}}
	for i, id := range []string{"done", "running", "queued"} {
		nb.Results = append(nb.Results, gentestrepo.Result{ID: id, BatchID: "crashed", Position: i, Media: "image", Model: "flux-2", Prompt: "p", SentPrompt: "p",
			NegativeMode: "none", CostBasis: "priced", CreatedAt: time.Now().UTC()})
	}
	require.NoError(t, r.Repo.CreateBatch(ctx, nb))
	_, _ = r.Repo.MarkRunning(ctx, "done")
	require.NoError(t, r.Repo.Settle(ctx, "done", gentestrepo.Settlement{Status: gentestrepo.StatusSucceeded, FilePath: "crashed/done.png", At: time.Now()}))
	_, _ = r.Repo.MarkRunning(ctx, "running")

	r.Svc.OnStartup(ctx)

	st, err := r.Svc.Batch(ctx, "crashed", true)
	require.NoError(t, err)
	got := map[string]string{}
	for _, x := range st.Results {
		got[x.ID] = x.Status
	}
	assert.Equal(t, map[string]string{"done": "succeeded", "running": "failed", "queued": "failed"}, got)
	assert.Equal(t, gentest.ReasonInterrupted, st.Results[1].Error)
	assert.True(t, st.Finished)
	assert.Equal(t, gentest.StateSucceeded, st.State)
	assert.Zero(t, len(r.Fake.Calls()), "nothing is resumed: a generation is paid for")
}

func TestAJobWhoseBatchWasDeletedEndsWithoutWork(t *testing.T) {
	r := gentesttest.New(t)
	require.True(t, r.Queue.Handles(domain.JobGentest))
	job, err := r.Queue.Enqueue(context.Background(), domain.JobGentest, map[string]string{"batch_id": "gone"})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		j, err := r.Jobs.Get(context.Background(), job.ID)
		require.NoError(t, err)
		return j.Status == domain.JobSucceeded
	}, 10*time.Second, 5*time.Millisecond)
}

// TestTheJobKindIsNotResumable: a gentest job that was running when mam died is failed by the queue's recovery, not run again
// (a generation is billed), and OnStartup then settles its results.
func TestTheJobKindIsNotResumable(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	table := repository.NewJobRepository(db)
	job, err := table.Create(ctx, domain.JobGentest, []byte(`{"batch_id":"b"}`))
	require.NoError(t, err)
	claimed, err := table.ClaimNext(ctx) // running, as a crash leaves it
	require.NoError(t, err)
	require.Equal(t, job.ID, claimed.ID)

	restarted := jobqueue.New(table, jobqueue.Options{})
	svc := gentest.New(gentest.Deps{Repo: gentestrepo.New(db), Jobs: restarted, JobTable: table, DataDir: t.TempDir()})
	svc.Register()
	resumed, failed, err := restarted.Recover(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, resumed)
	assert.Equal(t, 1, failed)
	got, _ := table.Get(ctx, job.ID)
	assert.Equal(t, domain.JobFailed, got.Status)
}
