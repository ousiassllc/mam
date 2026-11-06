package gentestrepo_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// #1079: the history is paged by the number of results the filter matches: the count is of the list that ListResults pages through (the same conditions; a
// Limit and an Offset do not narrow it).
func TestCountResultsCountsWhatTheFilterMatchesWhateverThePage(t *testing.T) {
	r, ctx := setup(t)
	vid := result("v1", 0, "kling-3.0", "wave")
	vid.Media = gentestrepo.MediaVideo
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "flux-2", "fox"), result("r2", 1, "nano-banana", "fox"))))
	require.NoError(t, r.CreateBatch(ctx, newBatch("b2", base.Add(time.Hour), vid)))
	require.NoError(t, r.CreateBatch(ctx, newBatch("b3", base.Add(2*time.Hour), result("r3", 0, "flux-2", "cat"))))
	require.NoError(t, r.Settle(ctx, "r1", gentestrepo.Settlement{Status: gentestrepo.StatusSucceeded, FilePath: "r1", At: base}))
	require.NoError(t, r.SetFavorite(ctx, "r1", true))

	count := func(f gentestrepo.Filter) int {
		n, err := r.CountResults(ctx, f)
		require.NoError(t, err)
		return n
	}
	assert.Equal(t, 4, count(gentestrepo.Filter{}))
	assert.Equal(t, 4, count(gentestrepo.Filter{Limit: 1, Offset: 3}), "a page does not narrow the count")
	assert.Equal(t, 2, count(gentestrepo.Filter{Model: "flux-2"}))
	assert.Equal(t, 3, count(gentestrepo.Filter{Models: []string{"flux-2", "kling-3.0"}}))
	assert.Equal(t, 1, count(gentestrepo.Filter{Media: gentestrepo.MediaVideo}))
	assert.Equal(t, 1, count(gentestrepo.Filter{FavoriteOnly: true}))
	assert.Equal(t, 1, count(gentestrepo.Filter{Model: "flux-2", FavoriteOnly: true}), "the conditions are all of them")
	assert.Equal(t, 0, count(gentestrepo.Filter{Model: "nano-banana", FavoriteOnly: true}))
	assert.Equal(t, 2, count(gentestrepo.Filter{BatchID: "b1"}))
	assert.Equal(t, 3, count(gentestrepo.Filter{Status: gentestrepo.StatusQueued}))
	assert.Equal(t, 2, count(gentestrepo.Filter{IDs: []string{"r2", "v1", "missing"}}))
	for _, f := range []gentestrepo.Filter{{}, {Model: "flux-2"}, {FavoriteOnly: true}, {Status: gentestrepo.StatusQueued}} {
		list, err := r.ListResults(ctx, f)
		require.NoError(t, err)
		assert.Len(t, list, count(f), "the count is the length of the whole list")
	}
}
