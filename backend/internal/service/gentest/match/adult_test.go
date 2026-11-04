package match_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

// adultRequest is a new match whose reference image is a picture of a suggestive pose series (Issue #1020).
func (r rig) adultRequest(mode string) match.Request {
	return match.Request{Mode: mode, Reference: gentest.RefInput{Source: gentest.FromPoseLibrary, ID: "sexy_S001_front"}, Model: "flux-2", TargetScore: 85, MaxRounds: 2}
}

// A match that was queued with the picture of a suggestive pose as its reference is not run once the age gate is closed (a job that starts after the
// gate was closed: one that waited in the queue, one that resumed): the picture is not read, the LLM is not asked, nothing is generated, and the match fails with
// a reason that names neither the pose nor the picture.
func TestAMatchQueuedWithAnAdultPoseIsNotRunOnceTheAgeGateIsClosed(t *testing.T) {
	lib := posestest.New(t)
	lib.InstallAdult(t)
	r := newRig(t, []int{90}, gentesttest.WithPoseLibrary(lib.Lib))
	r.Queue.Stop() // queued, not run: the job is run by hand below
	ref := r.start(r.adultRequest(match.ModeAuto))
	lib.Open.Store(false)

	payload, err := json.Marshal(record.Payload{BatchID: ref.BatchID})
	require.NoError(t, err)
	err = r.Svc.RunJob(context.Background(), domain.Job{Payload: payload})
	assert.ErrorIs(t, err, runner.ErrInputHidden)
	assert.Zero(t, r.LLM.Count(gentesttest.CallDescribe), "the picture was not shown to the LLM")
	assert.Empty(t, r.Fake.Calls(), "no picture was made")
	v, err := r.M.View(context.Background(), ref.BatchID)
	require.NoError(t, err)
	assert.True(t, v.Finished)
	for _, rd := range v.Rounds {
		assert.Equal(t, gentest.StatusFailed, rd.Status, "the rounds are settled, not left queued")
		assert.NotContains(t, rd.Error, "sexy_")
	}
}

// A match that is over goes on only while the picture it started from may be used: with the gate closed the estimate (and so the run) of the go-on is
// refused without naming the pose, and the gate opened again lets it go on.
func TestAMatchWithAnAdultPoseReferenceCannotGoOnWhileTheAgeGateIsClosed(t *testing.T) {
	lib := posestest.New(t)
	lib.InstallAdult(t)
	r := newRig(t, []int{90, 90}, gentesttest.WithPoseLibrary(lib.Lib))
	first := r.start(r.adultRequest(match.ModeOnce))
	r.finished(first.BatchID)
	llmCalls, pictures := len(r.LLM.Calls()), len(r.Fake.Calls())

	lib.Open.Store(false)
	goOn := match.Request{ContinueBatchID: first.BatchID, Mode: match.ModeOnce}
	_, err := r.M.Estimate(context.Background(), goOn)
	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.NotContains(t, invalid.Message, "sexy_")
	_, err = r.M.Run(context.Background(), goOn, gentest.Confirmation{})
	assert.ErrorAs(t, err, &invalid, "the run is refused the same way")
	assert.Equal(t, llmCalls, len(r.LLM.Calls()), "the LLM was not asked again")
	assert.Equal(t, pictures, len(r.Fake.Calls()), "and no picture was made")

	lib.Open.Store(true)
	e, err := r.M.Estimate(context.Background(), goOn)
	require.NoError(t, err)
	assert.Positive(t, e.Rounds)
}
