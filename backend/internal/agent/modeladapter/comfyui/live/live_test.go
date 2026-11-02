package live_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
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
