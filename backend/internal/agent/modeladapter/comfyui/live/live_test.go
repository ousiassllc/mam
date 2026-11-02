package live_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestLookupKnowsOnlyRunningJobsOfTheEndpoint(t *testing.T) {
	r := live.New(nil)
	job := r.Start("ep", "p1")
	_, ok := r.Lookup("ep", "p1")
	assert.True(t, ok)
	_, ok = r.Lookup("other", "p1")
	assert.False(t, ok, "the endpoint is part of the key")
	job.End()
	_, ok = r.Lookup("ep", "p1")
	assert.False(t, ok, "an ended job is gone")
}

func TestStepGivesTheProgressAndAnEstimateOfTheRestOfTheNode(t *testing.T) {
	c := &clock{t: time.Unix(1000, 0)}
	r := live.New(c.now)
	job := r.Start("ep", "p1")

	p, _ := r.Lookup("ep", "p1")
	assert.Zero(t, p.MaxStep, "no step is known before the server reports one")
	assert.False(t, p.HasETA)

	job.Step("7", "KSampler", 2, 20, 10)
	p, _ = r.Lookup("ep", "p1")
	assert.False(t, p.HasETA, "one message is no speed")

	c.t = c.t.Add(20 * time.Second)
	job.Step("7", "KSampler", 12, 20, 55)
	p, _ = r.Lookup("ep", "p1")
	require.True(t, p.HasETA)
	assert.Equal(t, "KSampler", p.Node)
	assert.Equal(t, 12, p.Step)
	assert.Equal(t, 20, p.MaxStep)
	assert.Equal(t, 55, p.Percent)
	assert.Equal(t, 16*time.Second, p.ETA, "10 steps took 20 s: 8 steps left take 16 s")

	// the next sampling node starts its own count and its own speed
	c.t = c.t.Add(time.Second)
	job.Step("9", "KSampler", 1, 10, 60)
	p, _ = r.Lookup("ep", "p1")
	assert.False(t, p.HasETA)
	assert.Equal(t, 10, p.MaxStep)
}

func TestANilJobIgnoresEverything(_ *testing.T) {
	var job *live.Job
	job.Step("1", "KSampler", 1, 2, 3)
	job.End()
}

func TestRenameFollowsAServerThatAnswersWithItsOwnPromptID(t *testing.T) {
	r := live.New(nil)
	job := r.Start("ep", "mine")
	job.Rename("theirs")
	_, ok := r.Lookup("ep", "mine")
	assert.False(t, ok)
	_, ok = r.Lookup("ep", "theirs")
	assert.True(t, ok)
	job.End()
	_, ok = r.Lookup("ep", "theirs")
	assert.False(t, ok)
}

func TestByOwnerFindsTheJobOfTheOwnerThatMovedLast(t *testing.T) {
	c := &clock{t: time.Unix(1000, 0)}
	r := live.New(c.now)
	_, ok := r.ByOwner("batch1")
	assert.False(t, ok)
	_, ok = r.ByOwner("")
	assert.False(t, ok, "an unowned job is nobody's")

	a, b, other := r.Start("ep1", "a"), r.Start("ep2", "b"), r.Start("ep1", "c")
	a.Own("batch1")
	b.Own("batch1")
	other.Own("batch2")
	a.Step("1", "KSampler", 3, 10, 30)
	c.t = c.t.Add(time.Second)
	b.Step("1", "KSamplerAdvanced", 5, 10, 50)
	c.t = c.t.Add(time.Second)
	other.Step("1", "KSampler", 9, 10, 90)

	p, ok := r.ByOwner("batch1")
	require.True(t, ok)
	assert.Equal(t, "b", p.PromptID, "of two jobs of the batch the one that moved last")
	assert.Equal(t, "KSamplerAdvanced", p.Node)

	b.End()
	p, ok = r.ByOwner("batch1")
	require.True(t, ok)
	assert.Equal(t, "a", p.PromptID)
	a.End()
	_, ok = r.ByOwner("batch1")
	assert.False(t, ok, "an ended job is gone")
}

func TestOwnerTravelsInTheContext(t *testing.T) {
	assert.Empty(t, live.OwnerOf(context.Background()))
	assert.Equal(t, "sh1", live.OwnerOf(live.WithOwner(context.Background(), "sh1")))
}

func TestOwnedByJobPutsTheQueueJobIDInTheContextOfTheHandler(t *testing.T) {
	var seen string
	handle := live.OwnedByJob(func(ctx context.Context, _ domain.Job) error {
		seen = live.OwnerOf(ctx)
		return errors.New("boom")
	})
	assert.EqualError(t, handle(context.Background(), domain.Job{ID: "job-7"}), "boom", "the handler's error is returned as it is")
	assert.Equal(t, "job-7", seen)
}

func TestViewIsNilBeforeTheFirstStepAndSaysWhyThereIsNoETA(t *testing.T) {
	assert.Nil(t, live.Progress{PromptID: "p"}.View(), "nothing is known while the model loads")

	v := live.Progress{Node: "KSampler", Step: 1, MaxStep: 4, Percent: 12}.View()
	require.NotNil(t, v)
	assert.Equal(t, 1, v.Step)
	assert.Equal(t, 4, v.MaxStep)
	assert.Equal(t, 12, v.Percent)
	assert.Nil(t, v.ETASeconds)
	assert.NotEmpty(t, v.ETAUnavailable)

	v = live.Progress{Node: "KSampler", Step: 2, MaxStep: 4, Percent: 40, ETA: 7600 * time.Millisecond, HasETA: true}.View()
	require.NotNil(t, v.ETASeconds)
	assert.Equal(t, 8, *v.ETASeconds, "rounded to the second")
	assert.Empty(t, v.ETAUnavailable)
}

// A node whose steps are all done has nothing left to wait for: no "not advanced yet" reason, even when no ETA could be told
// (the node was first seen at its last step).
func TestViewGivesNoReasonForMissingETAOfAFinishedNode(t *testing.T) {
	v := live.Progress{Node: "KSampler", Step: 4, MaxStep: 4, Percent: 60}.View()
	require.NotNil(t, v)
	assert.Nil(t, v.ETASeconds)
	assert.Empty(t, v.ETAUnavailable)
}
