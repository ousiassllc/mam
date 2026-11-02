package match_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

type rig struct {
	*gentesttest.Rig
	LLM *gentesttest.FakeMatchLLM
	M   *match.Service
}

func newRig(t *testing.T, scores []int, opts ...gentesttest.Option) rig {
	t.Helper()
	r := gentesttest.New(t, opts...)
	f := &gentesttest.FakeMatchLLM{Scores: scores, Err: map[string]error{}}
	return rig{Rig: r, LLM: f, M: r.UseMatch(f)}
}

// newPricedRig is newRig whose LLM routes name the given targets (the estimate prices them).
func newPricedRig(t *testing.T, create, judge llm.Target, opts ...gentesttest.Option) rig {
	t.Helper()
	r := gentesttest.New(t, opts...)
	f := &gentesttest.FakeMatchLLM{Err: map[string]error{}}
	return rig{Rig: r, LLM: f, M: r.UseMatchWith(f, create, judge)}
}

// request is a new match on flux-2 (0.03 USD a picture, takes a seed) with an uploaded reference.
func (r rig) request(mode string) match.Request {
	r.T.Helper()
	id, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(r.T, err)
	return match.Request{Mode: mode, Reference: gentest.RefInput{Source: gentest.FromStaged, ID: id}, Model: "flux-2", TargetScore: 85, MaxRounds: 5}
}

// start runs the request with the estimate it has now (what the confirm button sends).
func (r rig) start(req match.Request) gentest.RunRef {
	r.T.Helper()
	e, err := r.M.Estimate(context.Background(), req)
	require.NoError(r.T, err)
	ref, err := r.M.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	require.NoError(r.T, err)
	return ref
}

// finished waits until the match is over (nothing is running and nothing more will be made).
func (r rig) finished(batchID string) match.View {
	r.T.Helper()
	require.EventuallyWithT(r.T, func(c *assert.CollectT) {
		v, err := r.M.View(context.Background(), batchID)
		require.NoError(c, err)
		assert.True(c, v.Finished)
	}, 20*time.Second, 10*time.Millisecond, "the match did not finish")
	v, err := r.M.View(context.Background(), batchID)
	require.NoError(r.T, err)
	return v
}

func scores(v match.View) []int {
	var out []int
	for _, r := range v.Rounds {
		if r.Score != nil {
			out = append(out, *r.Score)
		}
	}
	return out
}
