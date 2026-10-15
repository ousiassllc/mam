package judge_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// gatedJobs stores the job like the real queue but holds the first Enqueue until release is closed,
// which keeps a Start between its "is a Judge pending?" check and its INSERT.
type gatedJobs struct {
	repo    *repository.JobRepository
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (j *gatedJobs) Enqueue(ctx context.Context, kind domain.JobKind, payload any) (domain.Job, error) {
	j.once.Do(func() { close(j.entered) })
	<-j.release
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, err
	}
	return j.repo.Create(ctx, kind, raw)
}

func TestStart_ConcurrentStartsOnATakeQueueOneJobAndTheOtherIsBusy(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{"shot_objective":"run"}')`,
			`INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES ('t1', 'pv1', 'kling-3.0', 'p1/a.mp4')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	jobs := &gatedJobs{repo: repository.NewJobRepository(db), entered: make(chan struct{}), release: make(chan struct{})}
	runner := judge.NewRunner(judge.Deps{Judges: repository.NewJudgeRepository(db), Jobs: jobs})

	first := make(chan error, 1)
	go func() { _, err := runner.Start(ctx, "t1"); first <- err }()
	<-jobs.entered // the first Start passed the pending check and is about to INSERT

	second := make(chan error, 1)
	go func() { _, err := runner.Start(ctx, "t1"); second <- err }()
	select {
	case err := <-second:
		assert.ErrorIs(t, err, judge.ErrBusy)
	case <-time.After(5 * time.Second):
		t.Fatal("the second Start went on to queue a Judge job of its own")
	}

	close(jobs.release)
	require.NoError(t, <-first)
	queued, err := repository.NewJobRepository(db).ListByStatus(ctx, domain.JobQueued)
	require.NoError(t, err)
	assert.Len(t, queued, 1, "one Judge job for the Take")
	_, err = runner.Start(ctx, "t1")
	assert.ErrorIs(t, err, judge.ErrBusy, "and later calls see it pending")
}
