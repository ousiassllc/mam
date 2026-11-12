package modelroutetest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// failureNotices are the chat messages that tell a failure (the progress cards of the starts are not).
func (r *rig) failureNotices() []string {
	var out []string
	for _, m := range r.chat.all() {
		if m.Kind != domain.KindProgress {
			out = append(out, m.Content)
		}
	}
	return out
}

// The Reference Packs of several Shots started together all fail for one cause (the sign-in ended while they waited): the thread says it once, and one more
// message names the other Shots (#1112), not one notice for each Shot.
func TestHandle_ShotsThatFailForTheSameCauseTogetherAreToldOnceAndTheRestAreNamedInOneNotice(t *testing.T) {
	r := newRigShots(t, nil, 3)
	ctx := context.Background()
	jobs := make([]domain.Job, 0, len(r.shotIDs))
	for _, id := range r.shotIDs {
		job, err := r.orch.StartReferencePack(ctx, id)
		require.NoError(t, err)
		jobs = append(jobs, job)
	}
	r.usable = nothingUsable

	for _, job := range jobs {
		require.Error(t, r.orch.Handle(ctx, job))
	}

	notices := r.failureNotices()
	require.Len(t, notices, 2, "the first failure, and one notice for the others: %v", notices)
	assert.Contains(t, notices[0], "Shot 1 のReference Pack の作成に失敗しました")
	assert.Contains(t, notices[0], "API キーを登録する", "the ways out are told once, with the cause")
	assert.Contains(t, notices[1], "ほかにも失敗しました（2 件）")
	assert.Contains(t, notices[1], "Shot 2 のReference Pack の作成、Shot 3 のReference Pack の作成")
	assert.Equal(t, 1, strings.Count(r.chat.text(), "使えるモデルがありません"), "the cause is not repeated for each Shot")
	images, _ := r.log.snapshot()
	assert.Empty(t, images, "the failure is not retried: nothing was generated")
}

// A failure after the burst ended (the user tries again and it fails the same way) is told again: the silence lasts only while the jobs of the burst run.
func TestHandle_AFailureOfTheSameCauseAfterTheBurstIsToldAgain(t *testing.T) {
	r := newRig(t, nil)
	ctx := context.Background()
	usable := r.usable
	for range 2 {
		r.usable = usable
		job, err := r.orch.StartReferencePack(ctx, r.shotID)
		require.NoError(t, err)
		r.usable = nothingUsable // the sign-in ended while the job waited in the queue
		require.Error(t, r.orch.Handle(ctx, job))
	}

	notices := r.failureNotices()
	require.Len(t, notices, 2, "each failure after a burst is told: %v", notices)
	for _, n := range notices {
		assert.Contains(t, n, "Shot 1 のReference Pack の作成に失敗しました")
	}
}

// The notice of the first failure could not be posted: nobody was told the cause, so the next Shot that fails for it tells it, instead of being summed up as
// "the same as the notice before" (#1112).
func TestHandle_ACauseWhoseFirstNoticeWasLostIsToldByTheNextFailure(t *testing.T) {
	r := newRigShots(t, nil, 3)
	ctx := context.Background()
	jobs := make([]domain.Job, 0, len(r.shotIDs))
	for _, id := range r.shotIDs {
		job, err := r.orch.StartReferencePack(ctx, id)
		require.NoError(t, err)
		jobs = append(jobs, job)
	}
	r.usable = nothingUsable
	r.chat.lose = 1

	for _, job := range jobs {
		require.Error(t, r.orch.Handle(ctx, job))
	}

	notices := r.failureNotices()
	require.Len(t, notices, 2, "the second failure tells the cause, one notice names the third: %v", notices)
	assert.Contains(t, notices[0], "Shot 2 のReference Pack の作成に失敗しました")
	assert.Contains(t, notices[0], "使えるモデルがありません")
	assert.Contains(t, notices[1], "ほかにも失敗しました（1 件）")
	assert.Contains(t, notices[1], "Shot 3 のReference Pack の作成")
}

// A job that could not be queued ends the burst when it is the last one open: the Shots that failed meanwhile for the cause already told are named then, not lost.
func TestStart_AQueueFailureThatEndsTheBurstTellsTheHeldSummary(t *testing.T) {
	r := newRigShots(t, nil, 3)
	ctx := context.Background()
	var jobs []domain.Job
	for _, id := range r.shotIDs[:2] {
		job, err := r.orch.StartReferencePack(ctx, id)
		require.NoError(t, err)
		jobs = append(jobs, job)
	}
	r.jobs.refuse = func() error { // while the third job is being queued the first two run and fail: it is the last one of the burst
		r.usable = nothingUsable
		for _, job := range jobs {
			assert.Error(t, r.orch.Handle(ctx, job))
		}
		return errors.New("queue is full")
	}

	_, err := r.orch.StartReferencePack(ctx, r.shotIDs[2])
	require.ErrorContains(t, err, "queue is full")

	notices := r.failureNotices()
	require.Len(t, notices, 2, "the first failure, and the summary held for the second: %v", notices)
	assert.Contains(t, notices[0], "Shot 1 のReference Pack の作成に失敗しました")
	assert.Contains(t, notices[1], "ほかにも失敗しました（1 件）")
	assert.Contains(t, notices[1], "Shot 2 のReference Pack の作成")
}

// RouteReports is what the announcement of an automatic start names: with no API key at all, the models that can be used are named with their route.
func TestRouteReports_NameTheModelsThatCanBeUsedWhenNoAPIKeyIsRegistered(t *testing.T) {
	r := newRig(t, nil)

	var image, video *modelroute.RouteReport
	reports := r.orch.RouteReports()
	for i := range reports {
		switch {
		case reports[i].Media == "image" && reports[i].Key == "new_reference":
			image = &reports[i]
		case reports[i].Media == "video" && reports[i].Key == string(domain.QualityBalanced):
			video = &reports[i]
		}
	}

	require.NotNil(t, image)
	require.NotNil(t, video)
	for _, rep := range []*modelroute.RouteReport{image, video} {
		assert.Equal(t, modelroute.StatusCondition, rep.Status)
		require.NotNil(t, rep.Use)
		assert.NotEqual(t, modelreg.RouteAPI, rep.Use.Route, "no API can be used")
		assert.NotEmpty(t, rep.Reason)
	}
}
